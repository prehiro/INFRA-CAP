package notes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Note is the row model for a single shared note (OneNote-style).
type Note struct {
	ID         int
	Title      string
	Content    string    // raw markdown source
	IsPrivate  bool
	AccentColor string   // custom color for the card's top stripe ("" = use default)
	CreatedBy  int
	CreatedAt  time.Time
	UpdatedBy  int
	UpdatedAt  time.Time
	// Derived (joined) — populated by List/Get for display
	CreatedByName string
	UpdatedByName string
}

// AllowedAccentColors is the strict whitelist of colors users can pick
// for the card's accent stripe. Empty string means "use default".
// We don't accept arbitrary CSS colors so a malicious user can't inject
// HTML/JS via the field (defence in depth — the form is also escaped).
var AllowedAccentColors = map[string]string{
	"":       "",
	"red":    "#dc2626",
	"orange": "#ea580c",
	"yellow": "#ca8a04",
	"green":  "#16a34a",
	"blue":   "#2563eb",
	"purple": "#9333ea",
}

// ResolveAccentColor accepts either a user-friendly key (e.g. "red")
// or the canonical hex (e.g. "#dc2626") and returns the canonical hex.
// Unknown values collapse to "". The hex form is what the client sends
// directly from the swatch buttons; the key form is accepted for
// backward compat with the original radio-based picker.
func ResolveAccentColor(input string) string {
	v := strings.ToLower(strings.TrimSpace(input))
	if v == "" {
		return ""
	}
	// exact key match
	if hex, ok := AllowedAccentColors[v]; ok {
		return hex
	}
	// hex match (case-insensitive)
	for _, hex := range AllowedAccentColors {
		if strings.EqualFold(hex, v) {
			return hex
		}
	}
	return ""
}

// Store is the DB layer for the notes module. All methods are safe for
// concurrent use; the underlying *sql.DB handles its own pooling.
type Store struct{ DB *sql.DB }

// ErrNotFound is returned by Get when no row matches the id.
var ErrNotFound = errors.New("note not found")

// Filter holds the query-string parameters for List. Q searches both
// title and content. ShowPrivate=1 includes the calling user's private
// notes (handler is responsible for verifying ownership before setting
// this). Limit caps the result set; pass 0 for no cap.
type Filter struct {
	Q           string
	CurrentUser int // used to decide which private notes to surface
	Limit       int
}

// List returns notes ordered by updated_at DESC, created_at DESC.
// Visibility rules:
//   - is_private = 0  -> visible to everyone authenticated
//   - is_private = 1  -> visible only to the creator
// Soft cap: if Limit > 0 and total exceeds it, only the first Limit
// rows are returned (caller can show a notice).
func (s *Store) List(ctx context.Context, f Filter) ([]Note, error) {
	q := strings.TrimSpace(f.Q)
	where := []string{
		"(n.is_private = 0 OR n.created_by = @p_current)",
	}
	args := []any{sql.Named("p_current", f.CurrentUser)}
	if q != "" {
		where = append(where, "(n.title LIKE @p_q OR n.content LIKE @p_q)")
		args = append(args, sql.Named("p_q", "%"+q+"%"))
	}
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	limitSQL := ""
	if f.Limit > 0 {
		limitSQL = "OFFSET 0 ROWS FETCH NEXT @p_limit ROWS ONLY"
		args = append(args, sql.Named("p_limit", f.Limit))
	}

	query := fmt.Sprintf(`
		SELECT
		    n.id, n.title, n.content, n.is_private, n.accent_color,
		    n.created_by, n.created_at, n.updated_by, n.updated_at,
		    ISNULL(creator.username, ''), ISNULL(updater.username, '')
		FROM notes n
		LEFT JOIN users creator  ON creator.id = n.created_by
		LEFT JOIN users updater  ON updater.id = n.updated_by
		%s
		ORDER BY n.updated_at DESC, n.created_at DESC
		%s
	`, whereSQL, limitSQL)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	var out []Note
	for rows.Next() {
		var n Note
		var accent sql.NullString
		if err := rows.Scan(
			&n.ID, &n.Title, &n.Content, &n.IsPrivate, &accent,
			&n.CreatedBy, &n.CreatedAt, &n.UpdatedBy, &n.UpdatedAt,
			&n.CreatedByName, &n.UpdatedByName,
		); err != nil {
			return nil, fmt.Errorf("scan note: %w", err)
		}
		if accent.Valid {
			n.AccentColor = accent.String
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Get returns a single note. Caller must check visibility (private notes
// must only be returned to their creator); this method does not enforce it.
func (s *Store) Get(ctx context.Context, id int) (*Note, error) {
	const q = `
		SELECT
		    n.id, n.title, n.content, n.is_private, n.accent_color,
		    n.created_by, n.created_at, n.updated_by, n.updated_at,
		    ISNULL(creator.username, ''), ISNULL(updater.username, '')
		FROM notes n
		LEFT JOIN users creator  ON creator.id = n.created_by
		LEFT JOIN users updater  ON updater.id = n.updated_by
		WHERE n.id = @p_id
	`
	var n Note
	var accent sql.NullString
	err := s.DB.QueryRowContext(ctx, q, sql.Named("p_id", id)).Scan(
		&n.ID, &n.Title, &n.Content, &n.IsPrivate, &accent,
		&n.CreatedBy, &n.CreatedAt, &n.UpdatedBy, &n.UpdatedAt,
		&n.CreatedByName, &n.UpdatedByName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get note: %w", err)
	}
	if accent.Valid {
		n.AccentColor = accent.String
	}
	return &n, nil
}

// Create inserts a new note and returns the assigned id. created_by and
// updated_by are both set to userID; created_at and updated_at use the
// DB default (SYSUTCDATETIME()). accentColor is "" by default (which
// means "use the type's default stripe color").
func (s *Store) Create(ctx context.Context, userID int, title, content string, isPrivate bool, accentColor string) (int, error) {
	const q = `
		INSERT INTO notes (title, content, is_private, accent_color, created_by, updated_by)
		VALUES (@p_title, @p_content, @p_private, @p_accent, @p_user, @p_user);
		SELECT CAST(SCOPE_IDENTITY() AS INT);
	`
	var accent sql.NullString
	if accentColor != "" {
		accent = sql.NullString{String: accentColor, Valid: true}
	}
	var id int
	err := s.DB.QueryRowContext(ctx, q,
		sql.Named("p_title", title),
		sql.Named("p_content", content),
		sql.Named("p_private", isPrivate),
		sql.Named("p_accent", accent),
		sql.Named("p_user", userID),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create note: %w", err)
	}
	return id, nil
}

// Update mutates an existing note. updated_by is set to userID; updated_at
// is set to DB now. created_by/created_at are NOT touched.
func (s *Store) Update(ctx context.Context, id, userID int, title, content string, isPrivate bool, accentColor string) error {
	const q = `
		UPDATE notes
		SET title = @p_title,
		    content = @p_content,
		    is_private = @p_private,
		    accent_color = @p_accent,
		    updated_by = @p_user,
		    updated_at = SYSUTCDATETIME()
		WHERE id = @p_id
	`
	var accent sql.NullString
	if accentColor != "" {
		accent = sql.NullString{String: accentColor, Valid: true}
	}
	res, err := s.DB.ExecContext(ctx, q,
		sql.Named("p_title", title),
		sql.Named("p_content", content),
		sql.Named("p_private", isPrivate),
		sql.Named("p_accent", accent),
		sql.Named("p_user", userID),
		sql.Named("p_id", id),
	)
	if err != nil {
		return fmt.Errorf("update note: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update note rows: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a note by id. Caller must verify ownership BEFORE calling
// (this method does not check created_by — handler enforces it).
func (s *Store) Delete(ctx context.Context, id int) error {
	const q = `DELETE FROM notes WHERE id = @p_id`
	res, err := s.DB.ExecContext(ctx, q, sql.Named("p_id", id))
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete note rows: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

package auth

import (
	"context"
	"log"
	"os"
	"strings"
)

// adminGID is the fixed GID for the seeded admin account.
const adminGID = "29384"

// defaultGID is what we store when an admin creates a user without
// specifying a GID. Internal-only app, so 'n/a' is a fine placeholder.
const defaultGID = "n/a"

// SeedFirstAdmin creates the initial admin account from env vars if no
// users exist. Env: INFRACAP_ADMIN_USER, INFRACAP_ADMIN_PASS, INFRACAP_ADMIN_NAME.
// The seeded admin's GID is hard-pinned to 29384 per Hiro.
func (s *Service) SeedFirstAdmin(ctx context.Context) {
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		log.Printf("seed admin: check users: %v", err)
		return
	}
	if count > 0 {
		return
	}
	user := os.Getenv("INFRACAP_ADMIN_USER")
	pass := os.Getenv("INFRACAP_ADMIN_PASS")
	name := os.Getenv("INFRACAP_ADMIN_NAME")
	if user == "" || pass == "" {
		log.Println("seed admin: no users exist and INFRACAP_ADMIN_USER/INFRACAP_ADMIN_PASS not set — login impossible")
		return
	}
	if name == "" {
		name = "Administrator"
	}
	hash, err := HashPassword(pass)
	if err != nil {
		log.Printf("seed admin: hash: %v", err)
		return
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, full_name, role, gid) VALUES (@p1, @p2, @p3, 'admin', @p4)`,
		user, hash, name, adminGID)
	if err != nil {
		log.Printf("seed admin: insert: %v", err)
		return
	}
	log.Printf("seed admin: created initial admin user %q (gid=%s)", user, adminGID)
}

// normalizeGID trims whitespace and forces uppercase. The GID is an
// identifier, not free text, so admins don't get to type 'emp001' and
// have the system treat it as different from 'EMP001'. Empty input
// becomes the 'n/a' placeholder.
func normalizeGID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultGID
	}
	return strings.ToUpper(s)
}

// List returns all users (admin view).
func (s *Service) List(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, username, password_hash, full_name, role, is_active, gid, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.IsActive, &u.GID, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserView is the lightweight projection of a user we need for the
// permissions overview — no password hash, no timestamps. Kept in
// its own type so the template doesn't see a User with a hash field.
type UserView struct {
	ID       int
	Username string
	FullName string
	Role     string
	IsActive bool
	GID      string
	// IsOnline = true when the user has at least one non-expired
	// session row in the sessions table. Drives the green/grey
	// status dot on the Active users list.
	IsOnline bool
}

// ListActiveViews returns active users only (for the permissions
// "users affected" section). Inactive users are hidden because
// they can't log in anyway — showing them would mislead the
// admin about who actually uses the configured access.
//
// IsOnline is computed by LEFT JOINing the sessions table and
// checking for any non-expired row for the user. The MAX(s.expires_at)
// > SYSUTCDATETIME() check is technically redundant with EXISTS but
// is clearer to read in the query plan.
func (s *Service) ListActiveViews(ctx context.Context) ([]UserView, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT u.id, u.username, u.full_name, u.role, u.is_active, u.gid,
		        CASE WHEN EXISTS(
		            SELECT 1 FROM sessions s
		            WHERE s.user_id = u.id AND s.expires_at > SYSUTCDATETIME()
		        ) THEN 1 ELSE 0 END
		 FROM users u
		 WHERE u.is_active = 1
		 ORDER BY u.role, u.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserView
	for rows.Next() {
		var u UserView
		var onlineBit int
		if err := rows.Scan(&u.ID, &u.Username, &u.FullName, &u.Role, &u.IsActive, &u.GID, &onlineBit); err != nil {
			return nil, err
		}
		u.IsOnline = onlineBit == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

// ActiveCountByRole returns the number of active users per role.
// Used to render the small "X users affected" badge on each role
// card in the permissions UI.
func (s *Service) ActiveCountByRole(ctx context.Context) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT role, COUNT(*) FROM users WHERE is_active = 1 GROUP BY role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"admin": 0, "user": 0}
	for rows.Next() {
		var r string
		var n int
		if err := rows.Scan(&r, &n); err != nil {
			return nil, err
		}
		out[r] = n
	}
	return out, rows.Err()
}

// Create inserts a new user. The GID is whatever the admin typed in the
// form; an empty input becomes 'n/a' (see normalizeGID). The full name
// is uppercased on save so 'hiro' and 'Hiro' don't end up mixed in the
// directory.
func (s *Service) Create(ctx context.Context, username, password, fullName, role, gid string) error {
	exists := 0
	s.DB.QueryRowContext(ctx, `SELECT CASE WHEN EXISTS(SELECT 1 FROM users WHERE username=@p1) THEN 1 ELSE 0 END`, username).Scan(&exists)
	if exists == 1 {
		return ErrDuplicateUsername
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	gid = normalizeGID(gid)
	fullName = strings.TrimSpace(strings.ToUpper(fullName))
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, full_name, role, gid) VALUES (@p1, @p2, @p3, @p4, @p5)`,
		username, hash, fullName, role, gid)
	return err
}

// Update modifies full name/role/active/gid; optional new password.
func (s *Service) Update(ctx context.Context, id int, fullName, role string, isActive bool, gid, newPassword string) error {
	gid = normalizeGID(gid)
	fullName = strings.TrimSpace(strings.ToUpper(fullName))
	if newPassword != "" {
		hash, err := HashPassword(newPassword)
		if err != nil {
			return err
		}
		_, err = s.DB.ExecContext(ctx,
			`UPDATE users SET full_name=@p1, role=@p2, is_active=@p3, gid=@p4, password_hash=@p5, updated_at=SYSUTCDATETIME() WHERE id=@p6`,
			fullName, role, isActive, gid, hash, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE users SET full_name=@p1, role=@p2, is_active=@p3, gid=@p4, updated_at=SYSUTCDATETIME() WHERE id=@p5`,
		fullName, role, isActive, gid, id)
	return err
}

// SetActive toggles the is_active flag for a user. Used by the
// /users/{id}/toggle-active action button.
func (s *Service) SetActive(ctx context.Context, id int, isActive bool) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE users SET is_active=@p1, updated_at=SYSUTCDATETIME() WHERE id=@p2`,
		isActive, id)
	return err
}

// IsUserOnline reports whether the given user has at least one
// non-expired session in the database. Used by the permissions UI
// to color the avatar status dot green vs grey.
func (s *Service) IsUserOnline(ctx context.Context, userID int) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT CASE WHEN EXISTS(
		    SELECT 1 FROM sessions WHERE user_id = @p1 AND expires_at > SYSUTCDATETIME()
		 ) THEN 1 ELSE 0 END`, userID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// GetByID fetches a single user.
func (s *Service) GetByID(ctx context.Context, id int) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, username, password_hash, full_name, role, is_active, gid FROM users WHERE id=@p1`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.IsActive, &u.GID)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetByUsername fetches a single user by username. Used by the create
// handler to look up the new id right after insert for the audit log.
func (s *Service) GetByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, username, password_hash, full_name, role, is_active, gid FROM users WHERE username=@p1`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.IsActive, &u.GID)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

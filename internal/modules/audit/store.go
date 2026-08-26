package audit

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Log is the row model for the audit trail.
type Log struct {
	ID        int
	ActorID   *int
	ActorName *string
	Action    string
	Entity    string
	EntityID  *string
	Changes   *string
	IP        *string
	CreatedAt time.Time
}

// Filter describes list query params for the audit page.
type Filter struct {
	Actor   string
	Action  string
	Entity  string
	From    *time.Time
	To      *time.Time
	Page    int
	PageSize int
}

// QueryString returns the active filter params as a URL query string (no leading ?).
func (f Filter) QueryString() string {
	v := url.Values{}
	if f.Actor != "" {
		v.Set("actor", f.Actor)
	}
	if f.Action != "" {
		v.Set("action", f.Action)
	}
	if f.Entity != "" {
		v.Set("entity", f.Entity)
	}
	if f.From != nil {
		v.Set("from", f.From.Format("2006-01-02"))
	}
	if f.To != nil {
		v.Set("to", f.To.Format("2006-01-02"))
	}
	return v.Encode()
}

var validAction = map[string]bool{
	"create": true, "update": true, "delete": true, "retired": true,
	"login": true, "logout": true, "export": true,
}

func (f *Filter) normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 500 {
		f.PageSize = 25
	}
	if f.Action != "" && !validAction[f.Action] {
		f.Action = ""
	}
}

func (f *Filter) whereClause() (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, val any) {
		args = append(args, val)
		n := len(args)
		conds = append(conds, strings.ReplaceAll(cond, "@p", fmt.Sprintf("@p%d", n)))
	}
	if a := strings.TrimSpace(f.Actor); a != "" {
		// match by actor name (case-insensitive)
		args = append(args, "%"+strings.ToLower(a)+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf("LOWER(actor_name) LIKE @p%d", n))
	}
	if f.Action != "" {
		add("action = @p", f.Action)
	}
	if e := strings.TrimSpace(f.Entity); e != "" {
		add("entity = @p", e)
	}
	if f.From != nil {
		add("created_at >= @p", *f.From)
	}
	if f.To != nil {
		// include the whole day
		add("created_at < @p", f.To.AddDate(0, 0, 1))
	}
	if len(conds) == 0 {
		return "1=1", args
	}
	return strings.Join(conds, " AND "), args
}

// Store handles audit log persistence + queries.
type Store struct{ DB *sql.DB }

const auditCols = `id, actor_id, actor_name, action, entity, entity_id, changes, ip, created_at`

func scanLog(row interface{ Scan(...any) error }) (*Log, error) {
	var l Log
	err := row.Scan(&l.ID, &l.ActorID, &l.ActorName, &l.Action, &l.Entity,
		&l.EntityID, &l.Changes, &l.IP, &l.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// List returns a page of audit rows (newest first) plus total count.
func (s *Store) List(ctx context.Context, f Filter) ([]*Log, int, error) {
	f.normalize()
	where, args := f.whereClause()

	var total int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (f.Page - 1) * f.PageSize
	q := fmt.Sprintf(`SELECT %s FROM audit_log WHERE %s
		ORDER BY created_at DESC OFFSET @p%d ROWS FETCH NEXT @p%d ROWS ONLY`,
		auditCols, where, len(args)+1, len(args)+2)
	args = append(args, offset, f.PageSize)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*Log
	for rows.Next() {
		l, err := scanLog(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}

// DistinctActors returns actor names that have logged activity (for the filter dropdown).
func (s *Store) DistinctActors(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT DISTINCT actor_name FROM audit_log WHERE actor_name IS NOT NULL ORDER BY actor_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Entities returns the distinct entities present in the log.
func (s *Store) Entities(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT DISTINCT entity FROM audit_log ORDER BY entity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

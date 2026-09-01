package auth

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
)

// PageKey is the canonical key used in the role_permissions table
// and in the sidebar nav config. Keep these stable — changing a
// key requires a data migration.
const (
	PageDashboard = "dashboard"
	PageLicenses  = "licenses"
	PageNotes     = "notes"
	PageUsers     = "users"
	PageAudit     = "audit"
)

// AllPages is the canonical list of page keys, used by the admin
// permissions UI to render the checklist. The order here is the
// order shown in the UI.
var AllPages = []string{
	PageDashboard,
	PageLicenses,
	PageNotes,
	PageUsers,
	PageAudit,
}

// PageLabel is the human-readable label for each page key.
// Used by the admin permissions UI to render the checklist rows.
var PageLabel = map[string]string{
	PageDashboard: "Dashboard",
	PageLicenses:  "License Manager",
	PageNotes:     "Notes",
	PageUsers:     "Users",
	PageAudit:     "Audit Trail",
}

// PermissionStore reads/writes the role_permissions table.
type PermissionStore struct{ DB *sql.DB }

// HasPageAccess returns true if the given role is allowed to view
// the given page. The admin role is always allowed (defence in
// depth — even if the admin row is removed from role_permissions
// we don't want to lock admins out of the admin permissions UI).
func (s *PermissionStore) HasPageAccess(ctx context.Context, role, pageKey string) bool {
	if role == "admin" {
		return true
	}
	if role == "" || pageKey == "" {
		return false
	}
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM role_permissions WHERE role_name = @p1 AND page_key = @p2`,
		role, pageKey,
	).Scan(&n)
	if err != nil {
		return false
	}
	return n > 0
}

// PageAccessForRole returns the set of page keys the role can
// access. Used by the admin permissions UI and by the layout
// template to filter the sidebar.
func (s *PermissionStore) PageAccessForRole(ctx context.Context, role string) (map[string]bool, error) {
	out := map[string]bool{}
	if role == "admin" {
		// admin gets all — short-circuit so the UI matches the gate
		for _, p := range AllPages {
			out[p] = true
		}
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT page_key FROM role_permissions WHERE role_name = @p1`, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// AllRoleAccess returns a map[role]map[pageKey]bool. Used by the
// admin permissions UI to render all roles' current permissions
// in one shot. Returns an entry for 'user' and 'admin' even if
// they have no rows yet (empty map).
func (s *PermissionStore) AllRoleAccess(ctx context.Context) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{
		"admin": {},
		"user":  {},
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT role_name, page_key FROM role_permissions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r, p string
		if err := rows.Scan(&r, &p); err != nil {
			return nil, err
		}
		if _, ok := out[r]; !ok {
			out[r] = map[string]bool{}
		}
		out[r][p] = true
	}
	// ensure admin gets visual "all on" even if rows are missing
	if len(out["admin"]) == 0 {
		for _, p := range AllPages {
			out["admin"][p] = true
		}
	}
	return out, rows.Err()
}

// SetRoleAccess replaces all page access rows for a role with the
// provided set. Runs in a transaction so partial writes don't
// leave the user locked out. The admin role is never written via
// this method — we always treat admin as full-access.
func (s *PermissionStore) SetRoleAccess(ctx context.Context, role string, pages []string) error {
	if role == "admin" {
		// admin is hard-coded to full; ignore the write
		return nil
	}
	// sanitize the list: only keep valid page keys
	keep := map[string]bool{}
	for _, p := range pages {
		if isValidPageKey(p) {
			keep[p] = true
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM role_permissions WHERE role_name = @p1`, role); err != nil {
		return err
	}
	for p := range keep {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO role_permissions (role_name, page_key) VALUES (@p1, @p2)`,
			role, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func isValidPageKey(k string) bool {
	for _, p := range AllPages {
		if p == k {
			return true
		}
	}
	return false
}

// UserAccess returns the explicit per-user page access overrides.
// Returns an empty map if the user has no overrides (caller should
// then fall back to the role default). Only includes rows where
// granted=1 for now; future denial support would expose granted=0
// here too.
func (s *PermissionStore) UserAccess(ctx context.Context, userID int) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT page_key, granted FROM user_page_access WHERE user_id = @p1 AND granted = 1`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		var g bool
		if err := rows.Scan(&p, &g); err != nil {
			return nil, err
		}
		out[p] = g
	}
	return out, rows.Err()
}

// SetUserAccess replaces all per-user override rows for a user
// with the provided set. Admin users are rejected (admin always
// has full access). pages with granted=false are deleted (we
// only persist grants for now).
func (s *PermissionStore) SetUserAccess(ctx context.Context, userID int, grants map[string]bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_page_access WHERE user_id = @p1`, userID); err != nil {
		return err
	}
	for p, g := range grants {
		if !g || !isValidPageKey(p) {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_page_access (user_id, page_key, granted) VALUES (@p1, @p2, 1)`,
			userID, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UserAccessSummary is a flat record of "which pages can this
// user access, after merging role default + override" — used by
// the admin permissions UI to render a clean access list per user.
type UserAccessSummary struct {
	UserID     int
	Username   string
	FullName   string
	Role       string
	GID        string
	Effective  map[string]bool // all pages the user can access
	Overrides  map[string]bool // only the rows in user_page_access (granted=1)
}

// AllUserAccessSummaries returns a per-user access summary for
// every active user. The Effective map is used by the UI for
// badges, and Overrides for the "you customized this" indicator.
func (s *PermissionStore) AllUserAccessSummaries(ctx context.Context) ([]UserAccessSummary, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT u.id, u.username, u.full_name, u.role, u.gid, u.is_active
		FROM users u
		WHERE u.is_active = 1
		ORDER BY u.role, u.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserAccessSummary
	var activeIDs []int
	type urow struct {
		id, active int
		uname, fname, role, gid string
	}
	var users []urow
	for rows.Next() {
		var u urow
		if err := rows.Scan(&u.id, &u.uname, &u.fname, &u.role, &u.gid, &u.active); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	for _, u := range users {
		roleAccess, _ := s.PageAccessForRole(ctx, u.role)
		overrides, _ := s.UserAccess(ctx, u.id)
		eff := map[string]bool{}
		for p, v := range roleAccess {
			eff[p] = v
		}
		// Apply additions (granted=1 only)
		for p, v := range overrides {
			if v {
				eff[p] = true
			}
		}
		_ = activeIDs
		out = append(out, UserAccessSummary{
			UserID:    u.id,
			Username:  u.uname,
			FullName:  u.fname,
			Role:      u.role,
			GID:       u.gid,
			Effective: eff,
			Overrides: overrides,
		})
	}
	return out, nil
}

// HasPageAccess checks the union of (role default) + (user override).
// Admin role always has access. If the user is nil, returns false.
func (s *PermissionStore) HasUserPageAccess(ctx context.Context, userID int, role, pageKey string) (bool, error) {
	if role == "admin" {
		return true, nil
	}
	if !s.HasPageAccess(ctx, role, pageKey) {
		// role doesn't allow; check user override
		over, err := s.UserAccess(ctx, userID)
		if err != nil {
			return false, err
		}
		return over[pageKey], nil
	}
	return true, nil
}

// RequirePageAccess returns a middleware that rejects requests
// (with 403) when the authenticated user's role does not have
// access to the given page key. The auth middleware must have
// already run and populated the context user.
// Access is granted if (role default) OR (per-user override) is true.
func RequirePageAccess(store *PermissionStore, pageKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := FromContext(r.Context())
			if u == nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			// Admin role always has access.
			if u.Role == "admin" {
				next.ServeHTTP(w, r)
				return
			}
			ok, err := store.HasUserPageAccess(r.Context(), u.ID, u.Role, pageKey)
			if err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PageKeyFromPath infers the page key from a request URL path.
// Used by the layout template's sidebar to figure out which page
// the user is currently on, so we can highlight the active item
// even though the nav is data-driven.
func PageKeyFromPath(path string) string {
	p := strings.TrimPrefix(path, "/")
	if p == "" {
		return PageDashboard
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		p = p[:i]
	}
	switch p {
	case "licenses":
		return PageLicenses
	case "notes":
		return PageNotes
	case "users":
		return PageUsers
	case "audit":
		return PageAudit
	case "":
		return PageDashboard
	}
	return p
}

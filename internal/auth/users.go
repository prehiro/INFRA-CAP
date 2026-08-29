package auth

import (
	"context"
	"log"
	"os"
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

// normalizeGID returns the value to store for the GID column. Whitespace
// gets trimmed; empty input becomes the default 'n/a' so the column is
// never NULL and the table always has something to show.
func normalizeGID(s string) string {
	if s == "" {
		return defaultGID
	}
	// Tidy common whitespace without reformatting the value.
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	if s == "" {
		return defaultGID
	}
	return s
}

// List returns all users (admin view).
func (s *Service) List(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, username, password_hash, full_name, role, is_active, gid FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.IsActive, &u.GID); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Create inserts a new user. The GID is whatever the admin typed in the
// form; an empty input becomes 'n/a' (see normalizeGID).
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
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, full_name, role, gid) VALUES (@p1, @p2, @p3, @p4, @p5)`,
		username, hash, fullName, role, gid)
	return err
}

// Update modifies full name/role/active/gid; optional new password.
func (s *Service) Update(ctx context.Context, id int, fullName, role string, isActive bool, gid, newPassword string) error {
	gid = normalizeGID(gid)
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

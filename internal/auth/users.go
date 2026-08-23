package auth

import (
	"context"
	"log"
	"os"
)

// SeedFirstAdmin creates the initial admin account from env vars if no users exist.
// Env: INFRACAP_ADMIN_USER, INFRACAP_ADMIN_PASS, INFRACAP_ADMIN_NAME.
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
		`INSERT INTO users (username, password_hash, full_name, role) VALUES (@p1, @p2, @p3, 'admin')`,
		user, hash, name)
	if err != nil {
		log.Printf("seed admin: insert: %v", err)
		return
	}
	log.Printf("seed admin: created initial admin user %q", user)
}

// List returns all users (admin view).
func (s *Service) List(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, username, password_hash, full_name, role, is_active FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.IsActive); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Create inserts a new user.
func (s *Service) Create(ctx context.Context, username, password, fullName, role string) error {
	exists := 0
	s.DB.QueryRowContext(ctx, `SELECT CASE WHEN EXISTS(SELECT 1 FROM users WHERE username=@p1) THEN 1 ELSE 0 END`, username).Scan(&exists)
	if exists == 1 {
		return ErrDuplicateUsername
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, full_name, role) VALUES (@p1, @p2, @p3, @p4)`,
		username, hash, fullName, role)
	return err
}

// Update modifies full name/role/active; optional new password.
func (s *Service) Update(ctx context.Context, id int, fullName, role string, isActive bool, newPassword string) error {
	if newPassword != "" {
		hash, err := HashPassword(newPassword)
		if err != nil {
			return err
		}
		_, err = s.DB.ExecContext(ctx,
			`UPDATE users SET full_name=@p1, role=@p2, is_active=@p3, password_hash=@p4, updated_at=SYSUTCDATETIME() WHERE id=@p5`,
			fullName, role, isActive, hash, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE users SET full_name=@p1, role=@p2, is_active=@p3, updated_at=SYSUTCDATETIME() WHERE id=@p4`,
		fullName, role, isActive, id)
	return err
}

// GetByID fetches a single user.
func (s *Service) GetByID(ctx context.Context, id int) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, username, password_hash, full_name, role, is_active FROM users WHERE id=@p1`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.IsActive)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

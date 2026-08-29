package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInactive           = errors.New("account is deactivated")
	ErrDuplicateUsername  = errors.New("username already exists")
)

type User struct {
	ID           int
	Username     string
	PasswordHash string
	FullName     string
	Role         string
	IsActive     bool
	GID          string
}

type Service struct {
	DB *sql.DB
}

// HashPassword hashes a plaintext password with bcrypt.
func HashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	return string(h), err
}

// CheckPassword compares plaintext against the stored hash.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Authenticate verifies credentials and creates a session. Returns session token.
func (s *Service) Authenticate(ctx context.Context, username, password string) (string, *User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, username, password_hash, full_name, role, is_active
		 FROM users WHERE username = @p1`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.IsActive)
	if errors.Is(err, sql.ErrNoRows) {
		// burn comparable time to avoid user-enumeration timing
		bcrypt.CompareHashAndPassword([]byte("$2a$12$0000000000000000000000000000000000000000000000000000"), []byte(password))
		return "", nil, ErrInvalidCredentials
	}
	if err != nil {
		return "", nil, err
	}
	if !u.IsActive {
		return "", nil, ErrInactive
	}
	if !CheckPassword(u.PasswordHash, password) {
		return "", nil, ErrInvalidCredentials
	}

	token, err := newToken()
	if err != nil {
		return "", nil, err
	}
	expires := time.Now().Add(24 * time.Hour)
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO sessions (token, user_id, expires_at) VALUES (@p1, @p2, @p3)`,
		token, u.ID, expires)
	if err != nil {
		return "", nil, err
	}
	return token, &u, nil
}

// SessionUser resolves a session token to its active user.
func (s *Service) SessionUser(ctx context.Context, token string) (*User, error) {
	var u User
	err := s.DB.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.password_hash, u.full_name, u.role, u.is_active
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token = @p1 AND s.expires_at > SYSUTCDATETIME() AND u.is_active = 1`, token).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FullName, &u.Role, &u.IsActive)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DestroySession removes a session (logout).
func (s *Service) DestroySession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token = @p1`, token)
	return err
}

// CleanupExpiredSessions removes expired sessions; safe to call periodically.
func (s *Service) CleanupExpiredSessions(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < SYSUTCDATETIME()`)
	return err
}

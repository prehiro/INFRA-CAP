package auth

import (
	"context"
	"net/http"
	"strings"

	"infracap/internal/web"
)

type ctxKey int

const userKey ctxKey = 0

const SessionCookie = "infracap_session"

// Middleware returns an http.Handler wrapper that resolves the session cookie
// and stores the *User in context. If require is true, unauthenticated
// requests are redirected to /login (HTMX requests get 401 + HX-Redirect).
// The provided permStore, if non-nil, is also attached to the context so
// the layout template's sidebar can use it to filter the nav by role.
func (s *Service) Middleware(require bool, permStore *PermissionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var user *User
			if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
				if u, err := s.SessionUser(r.Context(), c.Value); err == nil {
					user = u
				}
			}
			if user != nil {
				r = r.WithContext(context.WithValue(r.Context(), userKey, user))
			}
			// Attach the permission store so the layout can filter the
			// sidebar nav by role. We use a private type from the web
			// package to keep the context key namespaced.
			if permStore != nil {
				r = web.SetNavStore(r, permStore)
			}
			if require && user == nil {
				if htmx := r.Header.Get("HX-Request"); strings.EqualFold(htmx, "true") {
					w.Header().Set("HX-Redirect", "/login")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole wraps a handler so only users with one of the allowed roles pass.
// Must run after Middleware(true).
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := FromContext(r.Context())
			if u == nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			for _, role := range roles {
				if u.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, "403 Forbidden", http.StatusForbidden)
		})
	}
}

// FromContext returns the authenticated user, or nil.
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userKey).(*User)
	return u
}

// SetSessionCookie writes the session cookie on login.
func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // set true behind TLS in prod
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
}

// ClearSessionCookie removes the session cookie on logout.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// GetFullName/GetUsername/GetRole satisfy web.UserInfo.
func (u *User) GetFullName() string { return u.FullName }
func (u *User) GetUsername() string { return u.Username }
func (u *User) GetRole() string     { return u.Role }

func init() { web.SetUserContextKey(userKey) }

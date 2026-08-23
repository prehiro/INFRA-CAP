package web

import (
	"context"
	"embed"
	"html/template"
	"net/http"
)

//go:embed all:templates
var templateFS embed.FS

var tmpl *template.Template

func init() {
	tmpl = template.Must(template.ParseFS(templateFS,
		"templates/layouts/*.html",
		"templates/pages/*.html",
	))
}

// Render renders an app page inside the main (sidebar) layout.
func Render(w http.ResponseWriter, r *http.Request, title string, data any) {
	RenderNamed(w, r, "content", title, data)
}

// RenderNamed renders with an explicit content template name and injects
// the authenticated user (if any) for the topbar.
func RenderNamed(w http.ResponseWriter, r *http.Request, contentName, title string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tdata := map[string]any{
		"Title": title,
		"Cards": cardsOf(data),
		"Data":  data,
	}
	if u := UserFromContext(r.Context()); u != nil {
		tdata["AuthUser"] = u
	}
	if err := tmpl.ExecuteTemplate(w, "layout", tdata); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// RenderAuth renders a page inside the auth layout (login screen, no sidebar).
func RenderAuth(w http.ResponseWriter, title string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "auth_layout", map[string]any{
		"Title":    title,
		"Error":    fieldOf(data, "Error"),
		"Username": fieldOf(data, "Username"),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func cardsOf(data any) any {
	if m, ok := data.(map[string]any); ok {
		return m["Cards"]
	}
	return nil
}

func fieldOf(data any, key string) any {
	if m, ok := data.(map[string]any); ok {
		return m[key]
	}
	return nil
}

// UserInfo is the minimal shape the layout needs. auth.User satisfies it.
type UserInfo interface {
	GetFullName() string
	GetUsername() string
	GetRole() string
}

// userCtxKey is a package-level key; auth package injects via SetUserContextKey.
var userCtxKey interface{}

// SetUserContextKey lets another package register its context key for user lookup.
func SetUserContextKey(key interface{}) { userCtxKey = key }

// UserFromContext resolves the UserInfo from request context if present.
func UserFromContext(ctx context.Context) UserInfo {
	if userCtxKey == nil {
		return nil
	}
	u, _ := ctx.Value(userCtxKey).(UserInfo)
	return u
}

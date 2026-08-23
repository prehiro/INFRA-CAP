package web

import (
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
// contentTemplate optionally overrides the "content" template name
// (e.g. "new_user_content"); empty = default per-title lookup.
func Render(w http.ResponseWriter, title string, data any) {
	RenderNamed(w, "content", title, data)
}

// RenderNamed renders with an explicit content template name.
func RenderNamed(w http.ResponseWriter, contentName, title string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", map[string]any{
		"Title": title,
		"Cards": cardsOf(data),
		"Data":  data,
	}); err != nil {
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

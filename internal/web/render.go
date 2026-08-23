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

// Render renders a page inside the base layout. The page's "content"
// template receives the data map directly (e.g. .Title, .Cards).
func Render(w http.ResponseWriter, title string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", map[string]any{
		"Title": title,
		"Cards": cardsOf(data),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// cardsOf extracts the Cards field if the handler passed a map with one.
func cardsOf(data any) any {
	if m, ok := data.(map[string]any); ok {
		return m["Cards"]
	}
	return nil
}

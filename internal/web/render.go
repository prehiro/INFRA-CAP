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
	// templates are symlinked/copied into internal/web/templates by Makefile target `sync-templates`
	tmpl = template.Must(template.ParseFS(templateFS,
		"templates/layouts/*.html",
		"templates/partials/*.html",
		"templates/pages/*.html",
	))
}

// Render renders a page inside the base layout.
func Render(w http.ResponseWriter, title string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", map[string]any{
		"Title": title,
		"Data":  data,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

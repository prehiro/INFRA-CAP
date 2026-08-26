package audit

import (
	"net/http"
	"strconv"
	"time"

	"infracap/internal/web"

	"infracap/internal/auth"
)

type Module struct{ Store *Store }

func New(s *Store) *Module { return &Module{Store: s} }

func (m *Module) Name() string { return "audit" }

// actions + entities for the filter dropdowns
var actions = []string{"create", "update", "retired", "delete", "login", "logout", "export"}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /audit", m.list)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func parseDate(s string) *time.Time {
	s = trim(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02", "02-01-06", "02-01-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := Filter{
		Actor:    q.Get("actor"),
		Action:   q.Get("action"),
		Entity:   q.Get("entity"),
		From:     parseDate(q.Get("from")),
		To:       parseDate(q.Get("to")),
		Page:     atoi(q.Get("page")),
		PageSize: 25,
	}
	items, total, err := m.Store.List(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	actors, _ := m.Store.DistinctActors(r.Context())
	entities, _ := m.Store.Entities(r.Context())

	totalPages := (total + f.PageSize - 1) / f.PageSize
	data := map[string]any{
		"Items":      items,
		"Total":      total,
		"Page":       f.Page,
		"TotalPages": totalPages,
		"F":          f,
		"Actors":     actors,
		"Entities":   entities,
		"Actions":    actions,
	}
	if r.Header.Get("HX-Request") == "true" {
		web.RenderNamed(w, r, "audit_results_content", "Audit Trail", data)
		return
	}
	web.RenderNamed(w, r, "audit_content", "Audit Trail", data)
}

// actorFrom resolves the current user (if any) for logging convenience.
func actorFrom(r *http.Request) (int, string) {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.ID, u.FullName
	}
	return 0, "system"
}

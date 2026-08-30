package notes

import (
	"net/http"
	"strconv"
	"strings"

	"infracap/internal/audit"
	"infracap/internal/auth"
	"infracap/internal/web"
)

// Module is the notes module. Implements web.Module.
type Module struct {
	Store *Store
}

// New returns a Module bound to store.
func New(s *Store) *Module { return &Module{Store: s} }

// Name returns the module identifier used in audit log Entity column + sidebar.
func (m *Module) Name() string { return "notes" }

// RegisterRoutes attaches all notes routes to the protected mux.
// Auth middleware (set up in main.go) ensures every request here has a
// valid session.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /notes", m.list)
	mux.HandleFunc("GET /notes/new", m.newForm)
	mux.HandleFunc("POST /notes", m.create)
	mux.HandleFunc("GET /notes/{id}", m.view)
	mux.HandleFunc("GET /notes/{id}/edit", m.editForm)
	mux.HandleFunc("POST /notes/{id}", m.update)
	mux.HandleFunc("GET /notes/{id}/delete-confirm", m.deleteConfirm)
	mux.HandleFunc("POST /notes/{id}/delete", m.delete)
}

// list renders the card grid. No pagination (per Hiro v1). The HTMX
// branch returns just the grid fragment; the full-page branch returns
// the page with the search bar and header button.
func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	f := Filter{
		Q:           r.URL.Query().Get("q"),
		CurrentUser: u.ID,
	}
	items, err := m.Store.List(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := map[string]any{
		"Notes":   items,
		"Q":       f.Q,
		"CurrentUser": u,
	}
	// Live search returns just the grid
	if r.Header.Get("HX-Request") == "true" {
		web.RenderNamed(w, r, "notes_results_content", "Notes", data)
		return
	}
	web.RenderNamed(w, r, "notes_content", "Notes", data)
}

// newForm returns the empty editor modal. Anyone can create a note.
func (m *Module) newForm(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"N":        &Note{}, // empty
		"IsNew":    true,
		"Error":    "",
		"MaxTitle": 200,
	}
	web.RenderNamed(w, r, "note_modal_content", "New Note", data)
}

// create handles POST /notes. Validates + persists + audits + redirects
// to refresh the grid. Validation errors re-render the modal with
// .alert-error inside (so the layout listener keeps it open).
func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.PostFormValue("title"))
	content := r.PostFormValue("content")
	isPrivate := r.PostFormValue("is_private") == "1"
	// accent_color is the user-friendly key (e.g. "red"). We resolve it
	// to the canonical hex via the whitelist so we never store anything
	// the picker didn't offer.
	accentKey := r.PostFormValue("accent_color")
	accentColor := ResolveAccentColor(accentKey)

	errors := map[string]string{}
	if title == "" {
		errors["title"] = "Title is required"
	}
	if strings.TrimSpace(content) == "" {
		errors["content"] = "Content cannot be empty"
	}
	if len(errors) > 0 {
		data := map[string]any{
			"N":        &Note{Title: title, Content: content, IsPrivate: isPrivate, AccentColor: accentColor},
			"IsNew":    true,
			"Error":    errors,
			"MaxTitle": 200,
		}
		web.RenderNamed(w, r, "note_modal_content", "New Note", data)
		return
	}

	id, err := m.Store.Create(r.Context(), u.ID, title, content, isPrivate, accentColor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	audit.Log(r.Context(), m.Store.DB, audit.Entry{
		ActorID:  u.ID,
		Action:   "note.create",
		Entity:   m.Name(),
		EntityID: strconv.Itoa(id),
		IP:       r.RemoteAddr,
	})

	// Close modal + refresh the grid
	w.Header().Set("HX-Retarget", "#notes-results")
	w.Header().Set("HX-Reswap", "outerHTML")
	data := map[string]any{
		"Notes":       mustList(m, r, u.ID),
		"Q":           "",
		"CurrentUser": u,
	}
	web.RenderNamed(w, r, "notes_results_content", "Notes", data)
}

// view shows a read-only detail modal (rendered HTML from markdown).
// Visible to: anyone for public notes, owner only for private notes.
func (m *Module) view(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	n, err := m.Store.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if n.IsPrivate && n.CreatedBy != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	rendered := RenderMarkdown(n.Content)
	data := map[string]any{
		"N":        n,
		"Rendered": rendered,
		"IsView":   true,
	}
	web.RenderNamed(w, r, "note_view_modal_content", n.Title, data)
}

// editForm returns the editor modal pre-filled with the note. Visibility
// rules: public = anyone can edit, private = owner only. Update of
// updated_by/updated_at happens on save.
func (m *Module) editForm(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	n, err := m.Store.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Anyone can edit public notes. Only owner can edit private notes.
	if n.IsPrivate && n.CreatedBy != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	data := map[string]any{
		"N":        n,
		"IsNew":    false,
		"Error":    "",
		"MaxTitle": 200,
	}
	web.RenderNamed(w, r, "note_modal_content", "Edit Note", data)
}

// update handles POST /notes/{id}. Same validation as create. updated_by
// becomes the current user, updated_at is set to DB now.
func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	existing, err := m.Store.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if existing.IsPrivate && existing.CreatedBy != u.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	title := strings.TrimSpace(r.PostFormValue("title"))
	content := r.PostFormValue("content")
	isPrivate := r.PostFormValue("is_private") == "1"
	accentKey := r.PostFormValue("accent_color")
	accentColor := ResolveAccentColor(accentKey)

	errors := map[string]string{}
	if title == "" {
		errors["title"] = "Title is required"
	}
	if strings.TrimSpace(content) == "" {
		errors["content"] = "Content cannot be empty"
	}
	if len(errors) > 0 {
		data := map[string]any{
			"N":        &Note{ID: id, Title: title, Content: content, IsPrivate: isPrivate, AccentColor: accentColor, CreatedBy: existing.CreatedBy, CreatedAt: existing.CreatedAt},
			"IsNew":    false,
			"Error":    errors,
			"MaxTitle": 200,
		}
		web.RenderNamed(w, r, "note_modal_content", "Edit Note", data)
		return
	}

	if err := m.Store.Update(r.Context(), id, u.ID, title, content, isPrivate, accentColor); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	audit.Log(r.Context(), m.Store.DB, audit.Entry{
		ActorID:  u.ID,
		Action:   "note.update",
		Entity:   m.Name(),
		EntityID: strconv.Itoa(id),
		IP:       r.RemoteAddr,
	})

	// Close modal + refresh grid
	w.Header().Set("HX-Retarget", "#notes-results")
	w.Header().Set("HX-Reswap", "outerHTML")
	data := map[string]any{
		"Notes":       mustList(m, r, u.ID),
		"Q":           r.URL.Query().Get("q"),
		"CurrentUser": u,
	}
	web.RenderNamed(w, r, "notes_results_content", "Notes", data)
}

// deleteConfirm returns a styled confirmation modal.
func (m *Module) deleteConfirm(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	n, err := m.Store.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if n.CreatedBy != u.ID {
		http.Error(w, "only the creator can delete a note", http.StatusForbidden)
		return
	}
	data := map[string]any{"N": n}
	web.RenderNamed(w, r, "note_delete_confirm_content", "Delete Note", data)
}

// delete is owner-only. Admin does NOT get implicit permission. The
// handler verifies ownership BEFORE calling the store.
func (m *Module) delete(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	existing, err := m.Store.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if existing.CreatedBy != u.ID {
		http.Error(w, "only the creator can delete a note", http.StatusForbidden)
		return
	}
	if err := m.Store.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	audit.Log(r.Context(), m.Store.DB, audit.Entry{
		ActorID:  u.ID,
		Action:   "note.delete",
		Entity:   m.Name(),
		EntityID: strconv.Itoa(id),
		IP:       r.RemoteAddr,
	})

	// Refresh grid
	w.Header().Set("HX-Retarget", "#notes-results")
	w.Header().Set("HX-Reswap", "outerHTML")
	data := map[string]any{
		"Notes":       mustList(m, r, u.ID),
		"Q":           r.URL.Query().Get("q"),
		"CurrentUser": u,
	}
	web.RenderNamed(w, r, "notes_results_content", "Notes", data)
}

// mustList is a tiny convenience: re-query the list for the grid after
// a mutation. Errors are swallowed (the retarget header has already
// been set, the next page load will surface them).
func mustList(m *Module, r *http.Request, userID int) []Note {
	items, err := m.Store.List(r.Context(), Filter{CurrentUser: userID})
	if err != nil {
		return nil
	}
	return items
}

// strPtr kept for potential future use; currently unused.
// func strPtr(s string) *string { return &s }

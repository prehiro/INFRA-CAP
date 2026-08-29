package auth

import (
	"net/http"
	"strconv"

	"infracap/internal/web"
)

// AdminUsersModule serves /users (admin only CRUD).
type AdminUsersModule struct {
	Service *Service
}

func NewAdminUsersModule(s *Service) *AdminUsersModule { return &AdminUsersModule{Service: s} }

func (m *AdminUsersModule) Name() string { return "users" }

type userView struct {
	ID       int
	Username string
	FullName string
	Role     string
	IsActive bool
}

func toView(u *User) userView {
	return userView{u.ID, u.Username, u.FullName, u.Role, u.IsActive}
}

func (m *AdminUsersModule) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /users", m.list)
	mux.HandleFunc("GET /users/new", m.newForm)
	mux.HandleFunc("POST /users", m.create)
	mux.HandleFunc("GET /users/{id}/edit", m.editForm)
	mux.HandleFunc("POST /users/{id}", m.update)
}

// isHTMX reports whether the request came from HTMX (so we render the
// modal fragment instead of the full page fallback).
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func (m *AdminUsersModule) list(w http.ResponseWriter, r *http.Request) {
	users, err := m.Service.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]userView, 0, len(users))
	for i := range users {
		views = append(views, toView(&users[i]))
	}
	web.RenderNamed(w, r, "users_content", "Users", map[string]any{"Users": views})
}

func (m *AdminUsersModule) newForm(w http.ResponseWriter, r *http.Request) {
	tpl := "user_form_content"
	if isHTMX(r) {
		tpl = "user_modal_content"
	}
	web.RenderNamed(w, r, tpl, "New User", map[string]any{
		"User":  userView{},
		"IsNew": true,
		"Error": "",
	})
}

func (m *AdminUsersModule) create(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	fullName := r.PostFormValue("full_name")
	role := r.PostFormValue("role")
	if role != "admin" && role != "user" {
		role = "user"
	}
	err := m.Service.Create(r.Context(), username, password, fullName, role)
	if err != nil {
		// On error, re-render the modal WITH the error so user can correct.
		if isHTMX(r) {
			web.RenderNamed(w, r, "user_modal_content", "New User", map[string]any{
				"User":  userView{Username: username, FullName: fullName, Role: role},
				"IsNew": true,
				"Error": "Failed to save: username may already exist or password too weak.",
			})
			return
		}
		web.RenderNamed(w, r, "user_form_content", "New User", map[string]any{
			"User":  userView{Username: username, FullName: fullName, Role: role},
			"IsNew": true,
			"Error": "Failed to save: username may already exist or password too weak.",
		})
		return
	}
	if isHTMX(r) {
		m.refreshResults(w, r)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func (m *AdminUsersModule) editForm(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	u, err := m.Service.GetByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tpl := "user_form_content"
	if isHTMX(r) {
		tpl = "user_modal_content"
	}
	web.RenderNamed(w, r, tpl, "Edit User", map[string]any{
		"User":  toView(u),
		"IsNew": false,
		"Error": "",
	})
}

func (m *AdminUsersModule) update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	r.ParseForm()
	fullName := r.PostFormValue("full_name")
	role := r.PostFormValue("role")
	if role != "admin" && role != "user" {
		role = "user"
	}
	isActive := r.PostFormValue("is_active") == "on" || r.PostFormValue("is_active") == "1"
	newPass := r.PostFormValue("new_password")

	if err := m.Service.Update(r.Context(), id, fullName, role, isActive, newPass); err != nil {
		// On error, fetch user back + re-render modal with error.
		if isHTMX(r) {
			u, _ := m.Service.GetByID(r.Context(), id)
			view := userView{ID: id, FullName: fullName, Role: role, IsActive: isActive}
			if u != nil {
				view.Username = u.Username
			}
			web.RenderNamed(w, r, "user_modal_content", "Edit User", map[string]any{
				"User":  view,
				"IsNew": false,
				"Error": "Failed to save: " + err.Error(),
			})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if isHTMX(r) {
		m.refreshResults(w, r)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// refreshResults sends HX-Retarget/HX-Reswap so the #user-results table
// re-renders with the latest data, while the layout's afterRequest
// listener (also matched for #user-results) closes the modal.
func (m *AdminUsersModule) refreshResults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("HX-Retarget", "#user-results")
	w.Header().Set("HX-Reswap", "outerHTML")
	m.list(w, r)
}


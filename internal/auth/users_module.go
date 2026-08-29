package auth

import (
	"net/http"
	"strconv"

	"infracap/internal/audit"
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
	// HTMX callers (the modal-success refresh) want just the table fragment;
	// full-page navigation wants the wrapped page.
	tpl := "users_content"
	if isHTMX(r) {
		tpl = "user_results_content"
	}
	web.RenderNamed(w, r, tpl, "Users", map[string]any{"Users": views})
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

	// Look up the new user's id for the audit log.
	newID := 0
	if u, _ := m.Service.GetByUsername(r.Context(), username); u != nil {
		newID = u.ID
	}
	actorID, actorName := userActorInfo(r)
	audit.Log(r.Context(), m.Service.DB, audit.Entry{
		ActorID:   actorID,
		ActorName: actorName,
		Action:    "create",
		Entity:    "users",
		EntityID:  strconv.Itoa(newID),
		Changes: map[string]any{
			"id":        newID,
			"username":  username,
			"full_name": fullName,
			"role":      role,
			"is_active": true,
		},
		IP: audit.ClientIP(r),
	})

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

	// Snapshot before the update so the audit log can show a clean diff.
	oldUser, _ := m.Service.GetByID(r.Context(), id)

	if err := m.Service.Update(r.Context(), id, fullName, role, isActive, newPass); err != nil {
		// On error, fetch user back + re-render modal with error.
		if isHTMX(r) {
			view := userView{ID: id, FullName: fullName, Role: role, IsActive: isActive}
			if oldUser != nil {
				view.Username = oldUser.Username
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

	// Build a before/after diff so the audit detail modal can render
	// only the changed fields with the line-through red -> green pills.
	newUser, _ := m.Service.GetByID(r.Context(), id)
	var changes map[string]any
	if oldUser != nil && newUser != nil {
		changes = userDiff(oldUser, newUser, newPass != "")
	} else {
		changes = map[string]any{
			"id":        id,
			"full_name": fullName,
			"role":      role,
			"is_active": isActive,
		}
	}
	actorID, actorName := userActorInfo(r)
	audit.Log(r.Context(), m.Service.DB, audit.Entry{
		ActorID:   actorID,
		ActorName: actorName,
		Action:    "update",
		Entity:    "users",
		EntityID:  strconv.Itoa(id),
		Changes:   changes,
		IP:        audit.ClientIP(r),
	})

	if isHTMX(r) {
		m.refreshResults(w, r)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

// userActorInfo returns the current logged-in user's id + full name for
// audit logging. Mirrors licenses.actorInfo.
func userActorInfo(r *http.Request) (int, string) {
	if u := FromContext(r.Context()); u != nil {
		return u.ID, u.FullName
	}
	return 0, "system"
}

// userDiff returns a {before, after} map of only the user fields that
// changed. passwordChanged is reported as a one-way marker so the audit
// trail can show the password was rotated without leaking the new value.
func userDiff(old, new *User, passwordChanged bool) map[string]any {
	before := map[string]any{}
	after := map[string]any{}
	if old.FullName != new.FullName {
		before["full_name"] = old.FullName
		after["full_name"] = new.FullName
	}
	if old.Role != new.Role {
		before["role"] = old.Role
		after["role"] = new.Role
	}
	if old.IsActive != new.IsActive {
		before["is_active"] = old.IsActive
		after["is_active"] = new.IsActive
	}
	if passwordChanged {
		after["password"] = "rotated"
	}
	return map[string]any{"before": before, "after": after}
}

// refreshResults sends HX-Retarget/HX-Reswap so the #user-results table
// re-renders with the latest data, while the layout's afterRequest
// listener (also matched for #user-results) closes the modal.
func (m *AdminUsersModule) refreshResults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("HX-Retarget", "#user-results")
	w.Header().Set("HX-Reswap", "outerHTML")
	m.list(w, r)
}


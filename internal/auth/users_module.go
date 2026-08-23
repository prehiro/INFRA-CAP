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
	web.Render(w, "Users", map[string]any{"Users": views})
}

func (m *AdminUsersModule) newForm(w http.ResponseWriter, r *http.Request) {
	web.RenderNamed(w, "new_user_content", "New User", map[string]any{
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
		web.RenderNamed(w, "new_user_content", "New User", map[string]any{
			"User":  userView{Username: username, FullName: fullName, Role: role},
			"IsNew": true,
			"Error": "Gagal menyimpan: username mungkin sudah dipakai atau password terlalu lemah.",
		})
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
	web.RenderNamed(w, "edit_user_content", "Edit User", map[string]any{
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

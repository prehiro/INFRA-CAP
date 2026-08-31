package auth

import (
	"net/http"

	"infracap/internal/web"
)

// permissionsModule is the /admin/permissions admin page. It lets
// an admin choose which pages each role can access. The data is
// stored in the role_permissions table; the PermissionStore is the
// single source of truth.
type permissionsModule struct {
	store *PermissionStore
}

// NewPermissionsModule constructs the module. It is registered in
// cmd/server/main.go behind an admin-only middleware.
func NewPermissionsModule(store *PermissionStore) *permissionsModule {
	return &permissionsModule{store: store}
}

// RegisterRoutes wires GET (render) and POST (save) for
// /admin/permissions.
func (m *permissionsModule) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/permissions", m.index)
	mux.HandleFunc("POST /admin/permissions", m.save)
}

// index renders the checklist page.
func (m *permissionsModule) index(w http.ResponseWriter, r *http.Request) {
	all, err := m.store.AllRoleAccess(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	web.RenderNamed(w, r, "permissions_content", "Permissions", map[string]any{
		"Roles":      []string{"admin", "user"},
		"AllPages":   AllPages,
		"PageLabel":  PageLabel,
		"RoleAccess": all,
		"csrfField":  web.CSRFHiddenField(r),
	})
}

// save replaces the access set for one role. The POST body uses
// repeated `pages=foo&pages=bar` form values; we read with
// r.PostForm["pages"] to preserve the slice. The role is taken
// from the `role` field.
func (m *permissionsModule) save(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	role := r.PostFormValue("role")
	if role == "" || (role != "admin" && role != "user") {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}
	pages := r.PostForm["pages"]
	if err := m.store.SetRoleAccess(r.Context(), role, pages); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Redirect back to the same page (PRG pattern)
	http.Redirect(w, r, "/admin/permissions", http.StatusSeeOther)
}

package auth

import (
	"net/http"
	"net/url"

	"infracap/internal/web"
)

// permissionsModule is the /admin/permissions admin page. It lets
// an admin choose which pages each role can access. The data is
// stored in the role_permissions table; the PermissionStore is the
// single source of truth. The authService is used to look up
// the list of active users so the page can show "users affected".
type permissionsModule struct {
	store *PermissionStore
	auth  *Service
}

// NewPermissionsModule constructs the module. It is registered in
// cmd/server/main.go behind an admin-only middleware.
func NewPermissionsModule(store *PermissionStore, auth *Service) *permissionsModule {
	return &permissionsModule{store: store, auth: auth}
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
	// Look up the active user list so the page can show "users
	// affected" by these permission settings. We also pull a
	// per-role count for the badge on each role card.
	activeUsers, err := m.auth.ListActiveViews(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	roleCounts, err := m.auth.ActiveCountByRole(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	web.RenderNamed(w, r, "permissions_content", "Permissions", map[string]any{
		"Roles":      []string{"admin", "user"},
		"AllPages":   AllPages,
		"PageLabel":  PageLabel,
		"RoleAccess": all,
		"ActiveUsers": activeUsers,
		"RoleCounts": roleCounts,
		"csrfField":  web.CSRFHiddenField(r),
	})
	// (debug log removed for the redesign pass)
	_ = activeUsers
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
	// Redirect back to the same page (PRG pattern) with a success flash
	// that the layout template will render as a toast. The cookie value
	// is URL-encoded so we don't have to deal with the JSON quoting
	// semantics of html/template.
	http.SetCookie(w, &http.Cookie{
		Name:     "infracap_flash",
		Value:    url.QueryEscape("kind=success&text=Permissions+saved"),
		Path:     "/",
		MaxAge:   30,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin/permissions", http.StatusSeeOther)
}

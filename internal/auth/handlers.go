package auth

import (
	"html/template"
	"net/http"

	"infracap/internal/web"
)

// Module registers login/logout routes.
type Module struct {
	Service *Service
}

func NewModule(s *Service) *Module { return &Module{Service: s} }

func (m *Module) Name() string { return "auth" }

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", m.loginPage)
	mux.HandleFunc("POST /login", m.loginSubmit)
	mux.HandleFunc("POST /logout", m.logout)
}

func (m *Module) loginPage(w http.ResponseWriter, r *http.Request) {
	// already logged in → dashboard
	if u := FromContext(r.Context()); u != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	web.RenderAuth(w, "Login", map[string]any{"Error": ""})
}

func (m *Module) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := template.HTMLEscapeString(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	token, _, err := m.Service.Authenticate(r.Context(), username, password)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		web.RenderAuth(w, "Login", map[string]any{
			"Error": "Username atau password salah.",
			"Username": username,
		})
		return
	}
	SetSessionCookie(w, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (m *Module) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		m.Service.DestroySession(r.Context(), c.Value)
	}
	ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

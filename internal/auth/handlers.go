package auth

import (
	"html/template"
	"net/http"

	"infracap/internal/audit"
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

	token, u, err := m.Service.Authenticate(r.Context(), username, password)
	if err != nil {
		audit.Log(r.Context(), m.Service.DB, audit.Entry{
			ActorName: username, Action: "login_failure", Entity: "auth", EntityID: "",
			Changes: map[string]any{"result": "failure", "reason": err.Error()},
			IP: audit.ClientIP(r),
		})
		w.WriteHeader(http.StatusUnauthorized)
		web.RenderAuth(w, "Login", map[string]any{
			"Error":   "Username atau password salah.",
			"Username": username,
		})
		return
	}
	SetSessionCookie(w, token)
	audit.Log(r.Context(), m.Service.DB, audit.Entry{
		ActorID: u.ID, ActorName: u.FullName, Action: "login_success", Entity: "auth", EntityID: "",
		Changes: map[string]any{"result": "success"}, IP: audit.ClientIP(r),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (m *Module) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		if u := FromContext(r.Context()); u != nil {
			audit.Log(r.Context(), m.Service.DB, audit.Entry{
				ActorID: u.ID, ActorName: u.FullName, Action: "logout", Entity: "auth", EntityID: "",
				IP: audit.ClientIP(r),
			})
		}
		m.Service.DestroySession(r.Context(), c.Value)
	}
	ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

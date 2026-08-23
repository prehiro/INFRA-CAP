package dashboard

import (
	"net/http"

	"infracap/internal/web"
)

// Module is the dashboard module. v1: placeholder page + health handled at server level.
type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "dashboard" }

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		web.Render(w, r, "Dashboard", map[string]any{
			"Cards": []map[string]string{
				{"Label": "Total License", "Value": "—"},
				{"Label": "In Use", "Value": "—"},
				{"Label": "Available", "Value": "—"},
				{"Label": "Expiring ≤30d", "Value": "—"},
			},
		})
	})
}

package dashboard

import (
	"strconv"
	"net/http"

	"infracap/internal/modules/licenses"
	"infracap/internal/web"
)

// Module is the dashboard module. v1: placeholder page + health handled at server level.
type Module struct{ Store *licenses.Store }

func New() *Module                       { return &Module{} }
func NewWithStore(s *licenses.Store) *Module { return &Module{Store: s} }

func (m *Module) Name() string { return "dashboard" }

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		cards := []map[string]string{
			{"Label": "Total License", "Value": "0"},
			{"Label": "In Use", "Value": "0"},
			{"Label": "Available", "Value": "0"},
			{"Label": "Expiring ≤30d", "Value": "0"},
		}
		if m.Store != nil {
			if total, inUse, avail, expiring, err := m.Store.Stats(r.Context()); err == nil {
				cards[0]["Value"] = itoa(total)
				cards[1]["Value"] = itoa(inUse)
				cards[2]["Value"] = itoa(avail)
				cards[3]["Value"] = itoa(expiring)
			}
		}
		web.RenderNamed(w, r, "dashboard_content", "Dashboard", map[string]any{"Cards": cards})
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

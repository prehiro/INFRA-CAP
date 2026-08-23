package web

import "net/http"

// Module is the contract every feature package implements.
// Adding a new module = new package + one Register call in main.go.
type Module interface {
	Name() string
	RegisterRoutes(mux *http.ServeMux)
}

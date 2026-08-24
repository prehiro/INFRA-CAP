package licenses

import (	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"infracap/internal/web"
)

type Module struct{ Store *Store }

func New(s *Store) *Module { return &Module{Store: s} }

func (m *Module) Name() string { return "licenses" }

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /licenses", m.list)
	mux.HandleFunc("GET /licenses/new", m.newForm)
	mux.HandleFunc("POST /licenses", m.save)
	mux.HandleFunc("GET /licenses/{id}", m.viewLicense)
	mux.HandleFunc("GET /licenses/{id}/edit", m.editForm)
	mux.HandleFunc("POST /licenses/{id}", m.update)
	mux.HandleFunc("POST /licenses/{id}/delete", m.retire)
	mux.HandleFunc("GET /licenses/export", m.exportExcel)
}

// ---- helpers ----

func parseDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", "02-01-06", "02-01-2006"} {
		t, err := time.Parse(layout, s)
		if err == nil {
			return &t, nil
		}
	}
	return nil, errBadDate
}

var errBadDate = &dateError{}

type dateError struct{}

func (e *dateError) Error() string { return "Format tanggal tidak valid (gunakan DD-MM-YY)" }

func formToLicense(r *http.Request) *License {
	ptr := func(key string) *string {
		v := strings.TrimSpace(r.PostFormValue(key))
		if v == "" {
			return nil
		}
		return &v
	}
	l := &License{
		ID:            atoi(r.PostFormValue("id")),
		Maker:         r.PostFormValue("maker"),
		SoftwareName:  r.PostFormValue("software_name"),
		Version:       ptr("version"),
		LicenseKey:    ptr("license_key"),
		ActivationKey: ptr("activation_key"),
		AssignedTo:    ptr("assigned_to"),
		DeviceHostname: ptr("device_hostname"),
		DeviceSN:      ptr("device_sn"),
		Section:       ptr("section"),
		PONo:          ptr("po_no"),
		Status:        r.PostFormValue("status"),
		Remarks:       ptr("remarks"),
	}
	if l.Status == "" {
		l.Status = "Available"
	}
	l.ActivatedOn, _ = parseDate(r.PostFormValue("activated_on"))
	l.ExpiryDate, _ = parseDate(r.PostFormValue("expiry_date"))
	return l
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func filterFromRequest(r *http.Request) Filter {
	q := r.URL.Query()
	f := Filter{
		Query:    q.Get("q"),
		Status:   q.Get("status"),
		Section:  q.Get("section"),
		Sort:     q.Get("sort"),
		Order:    q.Get("order"),
		Page:     atoi(q.Get("page")),
		PageSize: 20,
	}
	var err error
	if f.ExpFrom, err = parseDate(q.Get("exp_from")); err != nil {
		f.ExpFrom = nil
	}
	if f.ExpTo, err = parseDate(q.Get("exp_to")); err != nil {
		f.ExpTo = nil
	}
	return f
}

// ---- handlers ----

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	f := filterFromRequest(r)
	items, total, err := m.Store.List(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	totalPages := (total + f.PageSize - 1) / f.PageSize
	data := map[string]any{
		"Items":      items,
		"Total":      total,
		"Page":       f.Page,
		"TotalPages": totalPages,
		"F":          f,
	}
	// HTMX live filter: return only the results fragment
	if r.Header.Get("HX-Request") == "true" {
		web.RenderNamed(w, r, "license_results_content", "License Manager", data)
		return
	}
	web.RenderNamed(w, r, "licenses_content", "License Manager", data)
}

// viewLicense shows a read-only detail page.
func (m *Module) viewLicense(w http.ResponseWriter, r *http.Request) {
	l, err := m.Store.GetByID(r.Context(), atoi(r.PathValue("id")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	web.RenderNamed(w, r, "license_form_content", "License Detail", map[string]any{
		"L":      l,
		"IsNew":  false,
		"Error":  "",
		"ViewOnly": true,
	})
}

func (m *Module) newForm(w http.ResponseWriter, r *http.Request) {
	m.renderForm(w, r, &License{Status: "Available"}, true, "")
}

func (m *Module) editForm(w http.ResponseWriter, r *http.Request) {
	l, err := m.Store.GetByID(r.Context(), atoi(r.PathValue("id")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	m.renderForm(w, r, l, false, "")
}

func (m *Module) renderForm(w http.ResponseWriter, r *http.Request, l *License, isNew bool, errMsg string) {
	web.RenderNamed(w, r, "license_form_content", formTitle(l, isNew), map[string]any{
		"L":     l,
		"IsNew": isNew,
		"Error": errMsg,
	})
}

func formTitle(l *License, isNew bool) string {
	if isNew {
		return "New License"
	}
	return "Edit License"
}

func (m *Module) save(w http.ResponseWriter, r *http.Request) {
	l := formToLicense(r)
	if err := m.Store.Save(r.Context(), l); err != nil {
		m.renderForm(w, r, l, l.ID == 0, err.Error())
		return
	}
	http.Redirect(w, r, "/licenses", http.StatusSeeOther)
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	l := formToLicense(r)
	l.ID = atoi(r.PathValue("id"))
	if err := m.Store.Save(r.Context(), l); err != nil {
		m.renderForm(w, r, l, false, err.Error())
		return
	}
	http.Redirect(w, r, "/licenses", http.StatusSeeOther)
}

// retire implements soft delete: status → Retired.
func (m *Module) retire(w http.ResponseWriter, r *http.Request) {
	id := atoi(r.PathValue("id"))
	l, err := m.Store.GetByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	l.Status = "Retired"
	l.AssignedTo = nil
	if err := m.Store.Save(r.Context(), l); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/licenses", http.StatusSeeOther)
}

var _ = context.Background

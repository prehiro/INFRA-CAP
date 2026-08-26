package licenses

import (	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"infracap/internal/audit"
	"infracap/internal/auth"
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
	mux.HandleFunc("GET /licenses/{id}/retire-confirm", m.retireConfirm)
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
		PageSize: 100000, // no pagination: show all rows
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
	statusCounts, _ := m.Store.StatusCounts(r.Context())
	data := map[string]any{
		"Items":      items,
		"Total":      total,
		"Page":       f.Page,
		"TotalPages": totalPages,
		"F":          f,
		"StatusCounts": statusCounts,
	}
	// HTMX live filter: return only the results fragment
	if r.Header.Get("HX-Request") == "true" {
		web.RenderNamed(w, r, "license_results_content", "License Manager", data)
		return
	}
	web.RenderNamed(w, r, "licenses_content", "License Manager", data)
}

// viewLicense shows a read-only detail (modal for HTMX, page fallback otherwise).
func (m *Module) viewLicense(w http.ResponseWriter, r *http.Request) {
	l, err := m.Store.GetByID(r.Context(), atoi(r.PathValue("id")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := map[string]any{"L": l, "IsNew": false, "Error": "", "ViewOnly": true}
	if r.Header.Get("HX-Request") == "true" {
		web.RenderNamed(w, r, "license_modal_content", "License Detail", data)
		return
	}
	web.RenderNamed(w, r, "license_form_content", "License Detail", data)
}

func (m *Module) newForm(w http.ResponseWriter, r *http.Request) {
	tpl := "license_form_content"
	if r.Header.Get("HX-Request") == "true" {
		tpl = "license_modal_content"
	}
	web.RenderNamed(w, r, tpl, "New License", map[string]any{
		"L": &License{Status: "Available"}, "IsNew": true, "Error": "",
	})
}

func (m *Module) editForm(w http.ResponseWriter, r *http.Request) {
	l, err := m.Store.GetByID(r.Context(), atoi(r.PathValue("id")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tpl := "license_form_content"
	if r.Header.Get("HX-Request") == "true" {
		tpl = "license_modal_content"
	}
	web.RenderNamed(w, r, tpl, "Edit License", map[string]any{
		"L": l, "IsNew": false, "Error": "",
	})
}

func (m *Module) renderForm(w http.ResponseWriter, r *http.Request, l *License, isNew bool, errMsg string) {
	tpl := "license_form_content"
	if r.Header.Get("HX-Request") == "true" {
		tpl = "license_modal_content"
	}
	web.RenderNamed(w, r, tpl, formTitle(l, isNew), map[string]any{
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
	isNew := l.ID == 0
	var old *License
	if !isNew {
		old, _ = m.Store.GetByID(r.Context(), l.ID)
	}
	if err := m.Store.Save(r.Context(), l); err != nil {
		m.renderForm(w, r, l, isNew, err.Error())
		return
	}
	actorID, actorName := actorInfo(r)
	if isNew {
		audit.Log(r.Context(), m.Store.DB, audit.Entry{
			ActorID: actorID, ActorName: actorName, Action: "create",
			Entity: "licenses", EntityID: strconv.Itoa(l.ID),
			Changes: licenseSnapshot(l), IP: audit.ClientIP(r),
		})
	} else {
		audit.Log(r.Context(), m.Store.DB, audit.Entry{
			ActorID: actorID, ActorName: actorName, Action: "update",
			Entity: "licenses", EntityID: strconv.Itoa(l.ID),
			Changes: diffLicenses(old, l), IP: audit.ClientIP(r),
		})
	}
	if r.Header.Get("HX-Request") == "true" {
		m.hxResults(w, r)
		return
	}
	http.Redirect(w, r, "/licenses", http.StatusSeeOther)
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	l := formToLicense(r)
	l.ID = atoi(r.PathValue("id"))
	old, _ := m.Store.GetByID(r.Context(), l.ID)
	if err := m.Store.Save(r.Context(), l); err != nil {
		m.renderForm(w, r, l, false, err.Error())
		return
	}
	actorID, actorName := actorInfo(r)
	audit.Log(r.Context(), m.Store.DB, audit.Entry{
		ActorID: actorID, ActorName: actorName, Action: "update",
		Entity: "licenses", EntityID: strconv.Itoa(l.ID),
		Changes: diffLicenses(old, l), IP: audit.ClientIP(r),
	})
	if r.Header.Get("HX-Request") == "true" {
		m.hxResults(w, r)
		return
	}
	http.Redirect(w, r, "/licenses", http.StatusSeeOther)
}

// hxResults returns the refreshed table fragment after a successful modal submit.
func (m *Module) hxResults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("HX-Retarget", "#license-results")
	w.Header().Set("HX-Reswap", "outerHTML")
	m.list(w, r)
}

// retireConfirm shows the styled confirmation modal for retiring a license.
func (m *Module) retireConfirm(w http.ResponseWriter, r *http.Request) {
	l, err := m.Store.GetByID(r.Context(), atoi(r.PathValue("id")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	web.RenderNamed(w, r, "license_retire_confirm_content", "Retire License",
		map[string]any{"L": l})
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
	actorID, actorName := actorInfo(r)
	audit.Log(r.Context(), m.Store.DB, audit.Entry{
		ActorID: actorID, ActorName: actorName, Action: "retired",
		Entity: "licenses", EntityID: strconv.Itoa(id),
		Changes: diffLicenses(l, &License{ID: l.ID, Maker: l.Maker, SoftwareName: l.SoftwareName,
			Status: "Retired", AssignedTo: nil, Version: l.Version, LicenseKey: l.LicenseKey,
			ActivationKey: l.ActivationKey, DeviceHostname: l.DeviceHostname, DeviceSN: l.DeviceSN,
			Section: l.Section, PONo: l.PONo, Remarks: l.Remarks,
			ActivatedOn: l.ActivatedOn, ExpiryDate: l.ExpiryDate}),
		IP: audit.ClientIP(r),
	})
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Retarget", "#license-results")
		w.Header().Set("HX-Reswap", "outerHTML")
		m.list(w, r)
		return
	}
	http.Redirect(w, r, "/licenses", http.StatusSeeOther)
}

// actorInfo resolves the current user id/name for audit logging.
func actorInfo(r *http.Request) (int, string) {
	if u := auth.FromContext(r.Context()); u != nil {
		return u.ID, u.FullName
	}
	return 0, "system"
}

// licenseSnapshot returns a flat map of the license's key fields for the audit trail.
func licenseSnapshot(l *License) map[string]any {
	return map[string]any{
		"id":             l.ID,
		"maker":          l.Maker,
		"software_name":  l.SoftwareName,
		"version":        derefOr(l.Version),
		"license_key":    derefOr(l.LicenseKey),
		"activation_key": derefOr(l.ActivationKey),
		"assigned_to":    derefOr(l.AssignedTo),
		"device_hostname": derefOr(l.DeviceHostname),
		"device_sn":      derefOr(l.DeviceSN),
		"section":        derefOr(l.Section),
		"po_no":          derefOr(l.PONo),
		"status":         l.Status,
		"activated_on":   dmyOr(l.ActivatedOn),
		"expiry_date":    dmyOr(l.ExpiryDate),
		"remarks":        derefOr(l.Remarks),
	}
}

// diffLicenses returns a {"before":{...},"after":{...}} map of only the changed fields.
func diffLicenses(old, new *License) map[string]any {
	if old == nil {
		return licenseSnapshot(new)
	}
	fields := []struct {
		key string
		get func(*License) any
	}{
		{"maker", func(l *License) any { return l.Maker }},
		{"software_name", func(l *License) any { return l.SoftwareName }},
		{"version", func(l *License) any { return derefOr(l.Version) }},
		{"license_key", func(l *License) any { return derefOr(l.LicenseKey) }},
		{"activation_key", func(l *License) any { return derefOr(l.ActivationKey) }},
		{"assigned_to", func(l *License) any { return derefOr(l.AssignedTo) }},
		{"device_hostname", func(l *License) any { return derefOr(l.DeviceHostname) }},
		{"device_sn", func(l *License) any { return derefOr(l.DeviceSN) }},
		{"section", func(l *License) any { return derefOr(l.Section) }},
		{"po_no", func(l *License) any { return derefOr(l.PONo) }},
		{"status", func(l *License) any { return l.Status }},
		{"activated_on", func(l *License) any { return dmyOr(l.ActivatedOn) }},
		{"expiry_date", func(l *License) any { return dmyOr(l.ExpiryDate) }},
		{"remarks", func(l *License) any { return derefOr(l.Remarks) }},
	}
	before, after := map[string]any{}, map[string]any{}
	for _, f := range fields {
		ov, nv := f.get(old), f.get(new)
		if !equalVal(ov, nv) {
			before[f.key] = ov
			after[f.key] = nv
		}
	}
	return map[string]any{"before": before, "after": after}
}

func equalVal(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func derefOr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func dmyOr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("02-01-06")
}

var _ = context.Background

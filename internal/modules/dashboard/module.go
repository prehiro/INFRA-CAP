package dashboard

import (
	"net/http"
	"strconv"
	"time"

	"infracap/internal/auth"
	"infracap/internal/modules/licenses"
	"infracap/internal/web"
)

// Module is the dashboard module. v1: real-time stats + expiring
// list, served by Go stdlib net/http.
type Module struct {
	Store *licenses.Store
	Auth  *auth.Service
}

func NewWithStore(s *licenses.Store) *Module { return &Module{Store: s} }
func New(s *licenses.Store, a *auth.Service) *Module {
	return &Module{Store: s, Auth: a}
}

func (m *Module) Name() string { return "dashboard" }

// StatCard is the data shape for a single stat tile.
type StatCard struct {
	Label string
	Value string
	Hint  string // e.g. "of 142 total"
	Tone  string // primary | success | warning | info | error | neutral
	Icon  string // lucide svg path d=""
	Pct   int    // 0-100 for the progress bar fill
}

// StatusSlice is one slice of the status-distribution donut/bar.
type StatusSlice struct {
	Label string
	Count int
	Tone  string
}

func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", m.index)
}

// index renders the dashboard. Pulls live counts from the licenses
// store so the cards reflect reality; falls back to zeros if the
// store isn't wired (shouldn't happen in prod, but the placeholder
// behaviour keeps the page from 500-ing during early boot).
func (m *Module) index(w http.ResponseWriter, r *http.Request) {
	cards := m.buildCards(r)
	expiring := m.buildExpiring(r)
	statusBreakdown := m.buildStatusBreakdown(r)
	greeting, username := m.buildGreeting(r)
	now := time.Now()
	weekday := now.Format("Monday")
	dateLong := now.Format("2 January 2006")

	data := map[string]any{
		"Cards":           cards,
		"ExpiringSoon":    expiring,
		"StatusBreakdown": statusBreakdown,
		"TotalLicenses":   sumCounts(statusBreakdown),
		"Greeting":        greeting,
		"Username":        username,
		"Weekday":         weekday,
		"DateLong":        dateLong,
		"Now":             now,
	}
	web.RenderNamed(w, r, "dashboard_content", "Dashboard", data)
}

func (m *Module) buildCards(r *http.Request) []StatCard {
	// defaults — show even if the store is missing
	cards := []StatCard{
		{Label: "Total Licenses", Value: "0", Tone: "primary", Icon: "layers"},
		{Label: "In Use", Value: "0", Tone: "success", Icon: "check-circle"},
		{Label: "Available", Value: "0", Tone: "info", Icon: "package"},
		{Label: "Expiring ≤ 30d", Value: "0", Tone: "warning", Icon: "clock"},
		{Label: "Expired", Value: "0", Tone: "error", Icon: "alert-triangle"},
		{Label: "Retired", Value: "0", Tone: "neutral", Icon: "archive"},
	}
	if m.Store == nil {
		// still populate percentages so the bars don't all read 0%
		for i := range cards {
			cards[i].Pct = 0
		}
		return cards
	}
	total, inUse, avail, expiring, expired, retired, err := m.Store.FullStats(r.Context())
	if err != nil {
		return cards
	}
	values := []int{total, inUse, avail, expiring, expired, retired}
	for i, v := range values {
		cards[i].Value = strconv.Itoa(v)
	}
	// percentages for each card (relative to the total)
	if total > 0 {
		for i, v := range values {
			cards[i].Pct = int(float64(v) / float64(total) * 100.0)
			if cards[i].Pct == 0 && v > 0 {
				// 1 of 1000 still shows a sliver of the bar
				cards[i].Pct = 1
			}
		}
	}
	// special hints for the most useful cards
	cards[0].Hint = "all records"
	cards[3].Hint = "needs attention"
	cards[4].Hint = "past expiry"
	cards[5].Hint = "archived"
	return cards
}

// buildExpiring returns up to 5 licenses that are within 60 days of
// expiry, with computed days_left for the dashboard widget.
func (m *Module) buildExpiring(r *http.Request) []ExpiringItem {
	if m.Store == nil {
		return nil
	}
	rows, err := m.Store.ExpiringSoon(r.Context())
	if err != nil || len(rows) == 0 {
		return nil
	}
	now := time.Now()
	limit := 5
	if len(rows) < limit {
		limit = len(rows)
	}
	out := make([]ExpiringItem, 0, limit)
	for _, l := range rows[:limit] {
		item := ExpiringItem{
			ID:           l.ID,
			SoftwareName: l.SoftwareName,
			Maker:        l.Maker,
			AssignedTo:   derefStr(l.AssignedTo),
			Status:       l.Status,
			Section:      derefStr(l.Section),
		}
		if l.ExpiryDate != nil {
			item.ExpiryDate = *l.ExpiryDate
			days := int(time.Until(*l.ExpiryDate).Hours() / 24)
			item.DaysLeft = days
			switch {
			case days < 0:
				item.Urgency = "expired"
			case days <= 7:
				item.Urgency = "critical"
			case days <= 30:
				item.Urgency = "warning"
			default:
				item.Urgency = "ok"
			}
		}
		_ = now
		out = append(out, item)
	}
	return out
}

func (m *Module) buildStatusBreakdown(r *http.Request) []StatusSlice {
	if m.Store == nil {
		return nil
	}
	counts, err := m.Store.StatusCounts(r.Context())
	if err != nil {
		return nil
	}
	// fixed display order so the donut / bar always lines up
	order := []struct {
		Label, Tone string
	}{
		{"In use", "success"},
		{"Available", "info"},
		{"Expired", "error"},
		{"Retired", "neutral"},
	}
	out := make([]StatusSlice, 0, len(order))
	for _, s := range order {
		out = append(out, StatusSlice{
			Label: s.Label,
			Tone:  s.Tone,
			Count: counts[s.Label],
		})
	}
	return out
}

func (m *Module) buildGreeting(r *http.Request) (string, string) {
	if m.Auth == nil {
		return "Welcome", "admin"
	}
	u := auth.FromContext(r.Context())
	if u == nil {
		return "Welcome", "admin"
	}
	name := u.FullName
	if name == "" {
		name = u.Username
	}
	h := time.Now().Hour()
	var greet string
	switch {
	case h < 5:
		greet = "Burning the midnight oil"
	case h < 12:
		greet = "Good morning"
	case h < 17:
		greet = "Good afternoon"
	case h < 21:
		greet = "Good evening"
	default:
		greet = "Working late"
	}
	return greet, name
}

func sumCounts(ss []StatusSlice) int {
	n := 0
	for _, s := range ss {
		n += s.Count
	}
	return n
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ExpiringItem is the dashboard's projection of a license that is
// about to expire. DaysLeft is precomputed (negative = past expiry).
type ExpiringItem struct {
	ID           int
	SoftwareName string
	Maker        string
	AssignedTo   string
	Status       string
	Section      string
	ExpiryDate   time.Time
	DaysLeft     int
	Urgency      string // expired | critical | warning | ok
}

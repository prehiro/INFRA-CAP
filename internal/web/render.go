package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"
)

//go:embed all:templates
var templateFS embed.FS

var tmpl *template.Template

var funcMap = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"trimSuffix": func(s, suf string) string { return strings.TrimSuffix(s, suf) },
	"sub": func(a, b int) int { return a - b },
	"div": func(a, b int64) int64 {
		if b == 0 {
			return 0
		}
		return a / b
	},
	"seq": func(start, end int) []int {
		if end < start || end-start > 200 {
			return nil
		}
		out := make([]int, 0, end-start+1)
		for i := start; i <= end; i++ {
			out = append(out, i)
		}
		return out
	},
	"now": func() time.Time { return time.Now() },
	"isExpiringSoon": func(t *time.Time, status string) bool {
		if t == nil || status == "Expired" || status == "Retired" {
			return false
		}
		return t.Before(time.Now().Add(30 * 24 * time.Hour))
	},
	"licenseStatuses": func() []string { return []string{"In use", "Available", "Expired", "Retired"} },
	// dict builds a map for template partial calls: {{template "x" (dict "A" 1 "B" "two")}}
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			if k, ok := kv[i].(string); ok {
				m[k] = kv[i+1]
			}
		}
		return m
	},
	// chipQuery builds a trusted filter query string for status chips (avoids
	// html/template stripping dynamically-built URLs in attribute context).
	"chipQuery": func(status, q, expFrom, expTo string) template.URL {
		v := url.Values{}
		if status != "" {
			v.Set("status", status)
		}
		if q != "" {
			v.Set("q", q)
		}
		if expFrom != "" {
			v.Set("exp_from", expFrom)
		}
		if expTo != "" {
			v.Set("exp_to", expTo)
		}
		if len(v) == 0 {
			return template.URL("")
		}
		return template.URL("?" + v.Encode())
	},
	// actionBadge renders a colored pill for an audit action. Returns template.HTML so the
	// markup is not escaped by html/template.
	"actionBadge": func(action string) template.HTML {
		cls := map[string]string{
			"create":  "badge-success",
			"update":  "badge-info",
			"retired": "badge-warning",
			"delete":  "badge-error",
			"login":   "badge-primary",
			"logout":  "badge-ghost",
			"export":  "badge-secondary",
		}
		c := cls[action]
		if c == "" {
			c = "badge-ghost"
		}
		return template.HTML(`<span class="badge badge-sm ` + c + `">` + action + `</span>`)
	},
	// diffView renders the audit Changes JSON as a professional before→after diff table.
	// Accepts either a flat snapshot (create/delete) or {before:{},after:{}} (update/retired).
	"diffView": func(raw any) template.HTML {
		var data []byte
		switch v := raw.(type) {
		case *string:
			if v == nil {
				return template.HTML("")
			}
			data = []byte(*v)
		case string:
			data = []byte(v)
		case json.RawMessage:
			data = v
		case []byte:
			data = v
		default:
			return template.HTML("")
		}
		var generic map[string]any
		if err := json.Unmarshal(data, &generic); err != nil {
			return template.HTML(`<pre class="text-xs whitespace-pre-wrap break-all font-mono bg-base-100 rounded-box p-3 border border-base-300 overflow-auto max-h-72">` + html.EscapeString(string(data)) + `</pre>`)
		}
		// Determine if it's a before/after diff.
		before, hasBefore := generic["before"].(map[string]any)
		after, hasAfter := generic["after"].(map[string]any)
		label := map[string]string{
			"maker": "Maker", "software_name": "Software Name", "version": "Version",
			"license_key": "License Key", "activation_key": "Activation Key",
			"assigned_to": "Assigned To", "device_hostname": "Hostname", "device_sn": "Device S/N",
			"section": "Section", "po_no": "PO No", "status": "Status",
			"activated_on": "Activated On", "expiry_date": "Expiry Date", "remarks": "Remarks",
		}
		var b strings.Builder
		esc := func(v any) string {
			if v == nil {
				return `<span class="text-base-content/30 italic">—</span>`
			}
			return html.EscapeString(fmt.Sprintf("%v", v))
		}
		if hasBefore && hasAfter {
			// collect union of keys preserving a stable order
			keys := []string{}
			seen := map[string]bool{}
			for k := range before {
				if !seen[k] {
					keys = append(keys, k)
					seen[k] = true
				}
			}
			for k := range after {
				if !seen[k] {
					keys = append(keys, k)
					seen[k] = true
				}
			}
			b.WriteString(`<div class="grid grid-cols-[7rem_1fr_1fr] gap-x-3 text-xs">`)
			b.WriteString(`<div class="font-semibold text-base-content/40 uppercase tracking-wide pb-1.5">Field</div>`)
			b.WriteString(`<div class="font-semibold text-error/70 uppercase tracking-wide pb-1.5">Before</div>`)
			b.WriteString(`<div class="font-semibold text-success/70 uppercase tracking-wide pb-1.5">After</div>`)
			for _, k := range keys {
				b.WriteString(`<div class="py-1 font-medium text-base-content/70">` + html.EscapeString(label[k]) + `</div>`)
				b.WriteString(`<div class="py-1 font-mono break-all line-through decoration-error/40">` + esc(before[k]) + `</div>`)
				b.WriteString(`<div class="py-1 font-mono break-all">` + esc(after[k]) + `</div>`)
			}
			b.WriteString(`</div>`)
		} else {
			// flat snapshot: key/value list
			b.WriteString(`<div class="grid grid-cols-[8rem_1fr] gap-x-3 text-xs">`)
			for k, v := range generic {
				b.WriteString(`<div class="py-1 font-medium text-base-content/70">` + html.EscapeString(label[k]) + `</div>`)
				b.WriteString(`<div class="py-1 font-mono break-all">` + esc(v) + `</div>`)
			}
			b.WriteString(`</div>`)
		}
		return template.HTML(b.String())
	},
	// queryStr builds a URL query string from an audit.Filter struct.
	"queryStr": func(f any) string {
		type filterer interface {
			QueryString() string
		}
		if ff, ok := f.(filterer); ok {
			return ff.QueryString()
		}
		return ""
	},
	"dmy": func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format("02-01-06")
	},
}

func init() {
	tmpl = template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS,
		"templates/layouts/*.html",
		"templates/pages/*.html",
	))
}

// Render renders an app page inside the main (sidebar) layout.
func Render(w http.ResponseWriter, r *http.Request, title string, data any) {
	RenderNamed(w, r, "content", title, data)
}

// RenderNamed renders the named content template first, then injects the
// resulting HTML into the layout (html/template cannot take dynamic template names).
func RenderNamed(w http.ResponseWriter, r *http.Request, contentName, title string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// HTMX request → return bare fragment (no layout wrapper)
	if r.Header.Get("HX-Request") == "true" {
		tdata := map[string]any{"Title": title, "csrfToken": CSRFFromRequest(r)}
		if m, ok := data.(map[string]any); ok {
			for k, v := range m {
				tdata[k] = v
			}
		}
		if err := tmpl.ExecuteTemplate(w, contentName, tdata); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	var contentBuf strings.Builder
	tdata := map[string]any{"Title": title}
	if m, ok := data.(map[string]any); ok {
		for k, v := range m {
			tdata[k] = v
		}
	}
	if err := tmpl.ExecuteTemplate(&contentBuf, contentName, tdata); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if u := UserFromContext(r.Context()); u != nil {
		tdata["AuthUser"] = u
	}
	tdata["csrfToken"] = CSRFFromRequest(r)
	tdata["ContentHTML"] = template.HTML(contentBuf.String())

	if err := tmpl.ExecuteTemplate(w, "layout", tdata); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// RenderAuth renders a page inside the auth layout (login screen).
func RenderAuth(w http.ResponseWriter, title string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "auth_layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func cardsOf(data any) any {
	if m, ok := data.(map[string]any); ok {
		return m["Cards"]
	}
	return nil
}

func fieldOf(data any, key string) any {
	if m, ok := data.(map[string]any); ok {
		return m[key]
	}
	return nil
}

// UserInfo is the minimal shape the layout needs. auth.User satisfies it.
type UserInfo interface {
	GetFullName() string
	GetUsername() string
	GetRole() string
}

// userCtxKey is a package-level key; auth package injects via SetUserContextKey.
var userCtxKey interface{}

// SetUserContextKey lets another package register its context key for user lookup.
func SetUserContextKey(key interface{}) { userCtxKey = key }

// UserFromContext resolves the UserInfo from request context if present.
func UserFromContext(ctx context.Context) UserInfo {
	if userCtxKey == nil {
		return nil
	}
	u, _ := ctx.Value(userCtxKey).(UserInfo)
	return u
}

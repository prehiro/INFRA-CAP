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
	// list returns a slice of the given values. Used to iterate a
	// static set of keys (e.g. accent color names) inside a template.
	"list": func(vs ...any) []any { return vs },
	// index returns the value at key k from a map. Convenience wrapper
	// around Go template's index function for typed map lookups.
	"index": func(m any, k any) any {
		switch mm := m.(type) {
		case map[string]any:
			return mm[fmt.Sprintf("%v", k)]
		case map[string]string:
			if s, ok := mm[fmt.Sprintf("%v", k)]; ok {
				return s
			}
			return ""
		}
		return ""
	},
	// safe marks a string as trusted HTML so html/template does not escape it.
	// Use ONLY for static markup authored by us (e.g. SVG icons, hx-get buttons
	// in partials). Never pass user input here.
	"safe": func(s string) template.HTML { return template.HTML(s) },
	// preview returns a short plain-text excerpt from markdown content for the
	// notes card grid. Strips heading markers, list bullets, blockquote chars,
	// and inline code/backtick markers; collapses whitespace; truncates to ~max
	// bytes at a word boundary. The Notes module is the only caller.
	"preview": notesPreview,
	// splitLines / splitWords return []string for the note footer stats.
	// (Length of the slice is the count, content of slice is unused in
	// the template — only the count is rendered.)
	"splitLines": func(s string) []string { return strings.Split(s, "\n") },
	"splitWords": func(s string) []string {
		fields := strings.Fields(s)
		if len(fields) == 0 {
			return nil
		}
		return fields
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
			"create":        "badge-success",
			"update":        "badge-info",
			"retired":       "badge-warning",
			"delete":        "badge-error",
			"login_success": "badge-success",
			"login_failure": "badge-error",
			"login":         "badge-primary",
			"logout":        "badge-ghost",
			"export":        "badge-secondary",
		}
		c := cls[action]
		if c == "" {
			c = "badge-ghost"
		}
		return template.HTML(`<span class="badge badge-soft badge-sm ` + c + `">` + action + `</span>`)
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
		var b strings.Builder
		// field label lookup
		label := map[string]string{
			"maker": "Maker", "software_name": "Software Name", "version": "Version",
			"license_key": "License Key", "activation_key": "Activation Key",
			"assigned_to": "Assigned To", "device_hostname": "Hostname", "device_sn": "Device S/N",
			"section": "Section", "po_no": "PO No", "status": "Status",
			"activated_on": "Activated On", "expiry_date": "Expiry Date", "remarks": "Remarks",
		}
		lab := func(k string) string {
			if l, ok := label[k]; ok {
				return l
			}
			return strings.ReplaceAll(k, "_", " ")
		}
		// stable field order
		order := []string{"maker", "software_name", "version", "status", "assigned_to",
			"device_hostname", "device_sn", "section", "license_key", "activation_key",
			"po_no", "activated_on", "expiry_date", "remarks"}
		esc := func(v any) string {
			if v == nil {
				return `<span class="text-base-content/30 italic">—</span>`
			}
			return html.EscapeString(fmt.Sprintf("%v", v))
		}
		if hasBefore && hasAfter {
			// order keys: those present, in preferred order, then any extras
			seen := map[string]bool{}
			keys := []string{}
			for _, k := range order {
				if _, ok := before[k]; ok {
					keys = append(keys, k)
					seen[k] = true
				}
				if _, ok := after[k]; ok {
					if !seen[k] {
						keys = append(keys, k)
						seen[k] = true
					}
				}
			}
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
			b.WriteString(`<ul class="space-y-2.5">`)
			for _, k := range keys {
				b.WriteString(`<li class="flex items-start gap-3 text-sm diff-row">`)
				b.WriteString(`<span class="w-28 shrink-0 pt-1 text-xs font-medium uppercase tracking-wide text-base-content/50">` + html.EscapeString(lab(k)) + `</span>`)
				b.WriteString(`<div class="flex flex-wrap items-center gap-2 min-w-0">`)
				b.WriteString(`<span class="font-mono text-xs px-2 py-1 rounded-md bg-error/10 text-error line-through decoration-error/50">` + esc(before[k]) + `</span>`)
				b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" class="w-3.5 h-3.5 text-base-content/40 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>`)
				b.WriteString(`<span class="font-mono text-xs px-2 py-1 rounded-md bg-success/10 text-success">` + esc(after[k]) + `</span>`)
				b.WriteString(`</div></li>`)
			}
			b.WriteString(`</ul>`)
		} else {
			// flat snapshot (create / export / login): definition list
			keys := []string{}
			seen := map[string]bool{}
			for _, k := range order {
				if _, ok := generic[k]; ok {
					keys = append(keys, k)
					seen[k] = true
				}
			}
			for k := range generic {
				if !seen[k] {
					keys = append(keys, k)
					seen[k] = true
				}
			}
			b.WriteString(`<dl class="space-y-2.5">`)
			for _, k := range keys {
				b.WriteString(`<div class="flex items-start gap-3 text-sm diff-row">`)
				b.WriteString(`<dt class="w-28 shrink-0 text-xs font-medium uppercase tracking-wide text-base-content/50">` + html.EscapeString(lab(k)) + `</dt>`)
				b.WriteString(`<dd class="font-mono text-xs px-2 py-1 rounded-md bg-base-200/60 break-all">` + esc(generic[k]) + `</dd>`)
				b.WriteString(`</div>`)
			}
			b.WriteString(`</dl>`)
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
	// pageWindow returns the page numbers to show in a compact pager.
	// 0 in the slice means an ellipsis gap.
	"pageWindow": func(cur, total int) []int {
		if total <= 7 {
			out := make([]int, total)
			for i := range out {
				out[i] = i + 1
			}
			return out
		}
		out := []int{1}
		start := cur - 2
		if start < 2 {
			start = 2
		}
		end := cur + 2
		if end > total-1 {
			end = total - 1
		}
		if start > 2 {
			out = append(out, 0) // ellipsis
		}
		for i := start; i <= end; i++ {
			out = append(out, i)
		}
		if end < total-1 {
			out = append(out, 0) // ellipsis
		}
		out = append(out, total)
		return out
	},
}

func init() {
	tmpl = template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS,
		"templates/layouts/*.html",
		"templates/pages/*.html",
		"templates/partials/*.html",
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

// notesPreview returns a short plain-text excerpt from markdown source for
// the notes card grid. Strips heading markers, list bullets, blockquote
// chars, and inline code/backtick markers; ALSO strips any raw HTML
// tags (e.g. <span style="color:#xxx"> from the color toolbar) so the
// card preview never displays literal markup. Collapses whitespace
// and truncates to ~max bytes at a word boundary. Used as a template
// helper so the web package does not need to import the notes module
// (which would create an import cycle since notes imports web for
// RenderNamed).
func notesPreview(content string, max int) string {
	if max <= 0 {
		max = 200
	}
	// First pass: strip raw HTML tags. The color toolbar in the edit modal
	// can produce inline <span style="color:#xxx">text</span> in the
	// markdown source. The card preview shows plain text only, so we
	// extract the inner text and drop the tag itself.
	var stripped strings.Builder
	inTag := false
	for _, r := range content {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			stripped.WriteRune(r)
		}
	}
	content = stripped.String()

	// Second pass: strip leading markdown markers per line.
	var out strings.Builder
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		for strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "-") ||
			strings.HasPrefix(t, "*") || strings.HasPrefix(t, "_") || strings.HasPrefix(t, "`") {
			t = t[1:]
		}
		// strip trailing closing markers like '**' or '__' that the loop
		// above would only have stripped from the leading side
		t = strings.TrimRight(t, "*_`")
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if out.Len() > 0 {
			out.WriteByte(' ')
		}
		out.WriteString(t)
	}
	s := out.String()
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return cut + "…"
}

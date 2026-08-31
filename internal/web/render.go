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
	"regexp"
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

// SetNavStore attaches a PermissionStore-like value to the request
// context so RenderNamed can use it when building the sidebar nav.
// The web package takes an interface (not a concrete *auth.PermissionStore)
// to avoid an import cycle — auth already imports web for SetNavStore
// from the other side. Any value that implements PageAccessForRole works.
type NavPermissionLister interface {
	PageAccessForRole(ctx context.Context, role string) (map[string]bool, error)
}

func SetNavStore(r *http.Request, s any) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), navStoreKey{}, s))
}

// navPermissionStore returns the value previously attached by
// SetNavStore. It is returned as the NavPermissionLister interface so
// the web package doesn't have to import the auth package (which
// itself imports web — would be a cycle). PermissionStore in the
// auth package satisfies this interface structurally.
func navPermissionStore(r *http.Request) NavPermissionLister {
	v, _ := r.Context().Value(navStoreKey{}).(NavPermissionLister)
	return v
}

// navItemsFor builds the sidebar menu list for the current user.
// Pages the role can't access are filtered out. Admin gets an extra
// "Permissions" entry pointing at the per-role management page. The
// returned slice is JSON-friendly (icons are inlined as safe HTML so
// the JS in the layout can render them with innerHTML).
func navItemsFor(access map[string]bool, role string) []any {
	// helper to make a nav entry
	mk := func(href, label, key, icon string) map[string]any {
		return map[string]any{
			"href":  href,
			"label": label,
			"key":   key,
			"icon":  template.HTML(icon),
		}
	}
	items := []any{}
	if access["dashboard"] {
		items = append(items, mk("/", "Dashboard", "dashboard", navIconDashboard))
	}
	if access["licenses"] {
		items = append(items, mk("/licenses", "License Manager", "licenses", navIconLicenses))
	}
	if access["notes"] {
		items = append(items, mk("/notes", "Notes", "notes", navIconNotes))
	}
	if access["users"] {
		items = append(items, mk("/users", "Users", "users", navIconUsers))
	}
	if access["audit"] {
		items = append(items, mk("/audit", "Audit Trail", "audit", navIconAudit))
	}
	if role == "admin" {
		items = append(items, mk("/admin/permissions", "Permissions", "permissions", navIconPermissions))
	}
	return items
}

// navItemsJSON marshals the nav items to a JSON string safe to
// inject into a <script> tag. We use json.Marshal directly (not
// template's JS context) so the icons — which contain real HTML
// (angle brackets, slashes) — are safely escaped as JSON strings.
func navItemsJSON(items []any) (template.JS, error) {
	// Convert to a stable shape (string fields) before marshaling so
	// the output is a predictable [{href,label,key,icon}, ...].
	type navItem struct {
		Href  string `json:"href"`
		Label string `json:"label"`
		Key   string `json:"key"`
		Icon  string `json:"icon"`
	}
	out := make([]navItem, 0, len(items))
	for _, raw := range items {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, navItem{
			Href:  asString(m["href"]),
			Label: asString(m["label"]),
			Key:   asString(m["key"]),
			Icon:  asString(m["icon"]),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return template.JS(b), nil
}

// asString coerces any value to a plain Go string. Used when
// reading the nav items map before JSON marshaling.
func asString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case template.HTML:
		return string(s)
	}
	return fmt.Sprint(v)
}

// navStoreKey is a private context key for the nav PermissionStore.
type navStoreKey struct{}

// Inline SVG icons for the sidebar. Kept short so the nav config
// stays a single Go file (the layout JS reads them as safe HTML).
const (
	navIconDashboard = `<svg xmlns="http://www.w3.org/2000/svg" class="w-full h-full" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="7" height="9" x="3" y="3" rx="1"/><rect width="7" height="5" x="14" y="3" rx="1"/><rect width="7" height="9" x="14" y="12" rx="1"/><rect width="7" height="5" x="3" y="16" rx="1"/></svg>`
	navIconLicenses  = `<svg xmlns="http://www.w3.org/2000/svg" class="w-full h-full" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/><path d="m9 12 2 2 4-4"/></svg>`
	navIconAudit     = `<svg xmlns="http://www.w3.org/2000/svg" class="w-full h-full" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/></svg>`
	navIconNotes     = `<svg xmlns="http://www.w3.org/2000/svg" class="w-full h-full" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="M9 13h6"/><path d="M9 17h4"/></svg>`
	navIconUsers     = `<svg xmlns="http://www.w3.org/2000/svg" class="w-full h-full" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>`
	navIconPermissions = `<svg xmlns="http://www.w3.org/2000/svg" class="w-full h-full" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>`
)

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
		// Sidebar nav config — filtered by the current user's role
		// page access. Build the list server-side so we never expose
		// links the user can't actually open.
		store := navPermissionStore(r)
		if store != nil {
			if access, err := store.PageAccessForRole(r.Context(), u.GetRole()); err == nil {
				items := navItemsFor(access, u.GetRole())
				if js, jerr := navItemsJSON(items); jerr == nil {
					tdata["NavItemsJS"] = js
				}
			}
		}
	}
	if tdata["NavItemsJS"] == nil {
		tdata["NavItemsJS"] = template.JS("[]")
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

// notesPreview returns a plain-text excerpt from markdown source for the
// notes card grid. Strips ALL markdown formatting so the card preview
// shows clean text. Preserves line breaks and limits to maxLines lines.
// Used as a template helper so the web package does not need to import
// the notes module (which would create an import cycle since notes
// imports web for RenderNamed).
func notesPreview(content string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 5
	}
	// Strip raw HTML tags (e.g. <span style="color:#xxx">text</span>).
	var buf strings.Builder
	inTag := false
	for _, r := range content {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			buf.WriteRune(r)
		}
	}
	content = buf.String()

	// Strip ALL inline markdown markers in one pass per line.
	var lines []string
	buf.Reset()
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		// heading markers: # / ## / ###
		for strings.HasPrefix(t, "# ") {
			t = t[2:]
		}
		// blockquote prefix: >
		for strings.HasPrefix(t, "> ") {
			t = t[2:]
		}
		// bullet prefix: - or *
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
			t = t[2:]
		}
		// ordered list prefix: 1. etc
		if len(t) > 2 && t[0] >= '0' && t[0] <= '9' {
			if idx := strings.Index(t, ". "); idx > 0 && idx < 4 {
				t = t[idx+2:]
			}
		}
		// horizontal rule: --- or *** or ___
		if rePreviewHr.MatchString(t) {
			continue
		}
		// Strip all inline formatting markers — order matters:
		// highlight first (== has no overlap with others), then code,
		// bold, italic, link.
		t = rePreviewHighlight.ReplaceAllString(t, "$1")
		t = rePreviewCode.ReplaceAllString(t, "$1")
		t = rePreviewBold.ReplaceAllString(t, "$1$2")
		t = rePreviewItalicStar.ReplaceAllString(t, "$1$2$3")
		t = rePreviewItalicUnderscore.ReplaceAllString(t, "$1$2$3")
		t = rePreviewLink.ReplaceAllString(t, "$1")
		// Clean up any residual == or ** markers from edge cases.
		t = strings.ReplaceAll(t, "==", "")
		t = strings.ReplaceAll(t, "``", "")
		// Strip standalone heading markers left over from == cleanup
		// (e.g. "text# heading" where == was around "# heading")
		t = rePreviewOrphanHeading.ReplaceAllString(t, " ")
		t = rePreviewMultiSpace.ReplaceAllString(t, " ")
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		lines = append(lines, t)
	}
	// Limit to maxLines lines.
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n")
}

// Regexps for stripping markdown in notesPreview (card grid plain text).
var (
	rePreviewHr               = regexp.MustCompile(`^[-*_]{3,}\s*$`)
	rePreviewHighlight        = regexp.MustCompile(`==([^=\n]+?)==`)
	rePreviewCode             = regexp.MustCompile("`([^`\n]+?)`")
	rePreviewBold             = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
	rePreviewItalicStar       = regexp.MustCompile(`(^|[^*])\*([^*\n]+?)\*([^*]|$)`)
	rePreviewItalicUnderscore = regexp.MustCompile(`(^|[^_])_([^_\n]+?)_([^_]|$)`)
	rePreviewLink             = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	// Strip heading markers left over after == cleanup (e.g. "text# heading")
	rePreviewOrphanHeading = regexp.MustCompile(`#+\s*`)
	rePreviewMultiSpace    = regexp.MustCompile(`\s{2,}`)
)

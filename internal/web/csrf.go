package web

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

const csrfCookie = "infracap_csrf"

// randomToken returns a 32-byte hex token.
func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
// CSRFCookieMiddleware ensures a CSRF cookie exists on every request.
func CSRFCookieMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(csrfCookie); err != nil {
			http.SetCookie(w, &http.Cookie{
				Name:     csrfCookie,
				Value:    randomToken(),
				Path:     "/",
				SameSite: http.SameSiteLaxMode,
			})
		}
		next.ServeHTTP(w, r)
	})
}

// CSRFFromRequest returns the token from the cookie (for embedding in forms).
func CSRFFromRequest(r *http.Request) string {
	if c, err := r.Cookie(csrfCookie); err == nil {
		return c.Value
	}
	return ""
}

// CSRFValidate validates double-submit token on state-changing requests.
// Safe methods are skipped. HTMX requests may send it via X-CSRF-Token header.
func CSRFValidate(next http.Handler) http.Handler {
	safe := map[string]bool{"GET": true, "HEAD": true, "OPTIONS": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if safe[r.Method] {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(csrfCookie)
		if err != nil || c.Value == "" {
			http.Error(w, "CSRF token missing", http.StatusForbidden)
			return
		}
		sent := r.PostFormValue("_csrf")
		if sent == "" {
			sent = r.Header.Get("X-CSRF-Token")
		}
		if sent == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			// ParseForm fallback for handlers that haven't parsed yet
			if err := r.ParseForm(); err == nil {
				sent = r.Form.Get("_csrf")
			}
		}
		if sent == "" || !validToken(c.Value, sent) {
			http.Error(w, "CSRF token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validToken(cookie, sent string) bool {
	// constant-time compare via subtle
	if len(cookie) != len(sent) {
		return false
	}
	var v byte
	for i := 0; i < len(cookie); i++ {
		v |= cookie[i] ^ sent[i]
	}
	return v == 0
}

// CSRFHiddenField returns an HTML hidden input with the CSRF token (template.HTML so it renders raw).
func CSRFHiddenField(r *http.Request) template.HTML {
	return template.HTML(`<input type="hidden" name="_csrf" value="` + url.QueryEscape(CSRFFromRequest(r)) + `">`)
}

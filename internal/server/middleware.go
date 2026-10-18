package server

import (
	"net/http"
	"strings"
)

const labbonetHeader = "X-Labbonet"

// Phase 1 CSP, pinned verbatim from SECURITY-MODEL.md section 4.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data: blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; " +
	"frame-ancestors 'none'; form-action 'self'"

// securityHeaders sets the fixed response headers required on every
// response (SECURITY-MODEL.md section 4). It never sets an
// Access-Control-Allow-* header.
func securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
}

// withSecurityHeaders wraps a handler so every response, including error
// responses from earlier middleware, carries the fixed security headers.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		next.ServeHTTP(w, r)
	})
}

// hostAllowlist rejects any request whose Host header is not this backend's
// own loopback address (S1), stopping DNS rebinding.
func hostAllowlist(allowedHosts map[string]bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHosts[r.Host] {
			w.WriteHeader(http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiOrigin enforces the Origin and Sec-Fetch-Site checks, the custom
// header requirement, and the session cookie requirement on every /api
// request (S2, S3, S5; SECURITY-MODEL.md section 4). No CORS headers are
// ever emitted, including on preflight OPTIONS requests (S4).
func apiOrigin(origin string, sessions *SessionManager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && o != origin {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" && sfs != "same-origin" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Header.Get(labbonetHeader) != "1" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if !sessions.ValidSessionCookie(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isAPIPath reports whether a path falls under /api.
func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

package handler

import (
	"net/http"
	"strings"
)

// SecurityHeadersMiddleware sets baseline browser-protection headers on
// responses for routes that render the app's own UI (admin, login, profile,
// uploads, API, project pages). Doc-serving routes are exempt because the
// HTML is uploaded by editors and may legitimately use inline scripts,
// embed other origins, or be embedded in iframes — applying the headers
// would break those use cases.
//
// Headers set:
//   - X-Content-Type-Options: nosniff
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - Content-Security-Policy: frame-ancestors 'self' (clickjacking)
//
// routePrefix matches what the mux is registered with: empty when a reverse
// proxy strips the base path, otherwise the configured base_path.
func SecurityHeadersMiddleware(routePrefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isDocContentPath(r, routePrefix) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", "frame-ancestors 'self'")
		}
		next.ServeHTTP(w, r)
	})
}

// isDocContentPath reports whether the request targets handleServeDoc:
// GET /{prefix}/project/{slug}/{version}/{path...} with at least one
// non-empty segment past the version. The action routes that share the
// /project/ prefix (/project/{slug}/version/{tag}/{download|delete|pin})
// have the literal segment "version" in position 1 and are excluded so
// headers still apply to them.
func isDocContentPath(r *http.Request, routePrefix string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	p := strings.TrimPrefix(r.URL.Path, routePrefix)
	if !strings.HasPrefix(p, "/project/") {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(p, "/project/"), "/", 3)
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return false
	}
	if parts[1] == "version" {
		return false
	}
	return true
}

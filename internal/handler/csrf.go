package handler

import "net/http"

// verifyCSRF checks the request's CSRF token against the per-session HMAC.
// The token is read from (in order) the X-CSRF-Token header, the parsed
// POST form, or the URL query string. Order matters because we want to
// avoid parsing the body when the caller has provided the token by header
// — important for multipart uploads where the handler controls the body
// limit itself.
//
// For multipart handlers that have already called r.ParseMultipartForm,
// r.PostForm is populated and the form-field path works. For URL-encoded
// POSTs the field path also works (ParseForm is idempotent).
//
// Returns false for anonymous requests (no session cookie) and for
// requests whose token doesn't match.
func (h *Handler) verifyCSRF(r *http.Request) bool {
	if header := r.Header.Get("X-CSRF-Token"); header != "" {
		return h.sessionMgr.VerifyCSRF(r, header)
	}
	if r.PostForm != nil {
		if v := r.PostForm.Get("csrf_token"); v != "" {
			return h.sessionMgr.VerifyCSRF(r, v)
		}
	}
	// Avoid implicit body parsing for handlers that haven't done it yet —
	// just fall through to FormValue which will ParseForm with the default
	// 32MB cap. Safe for URL-encoded forms; multipart handlers should call
	// ParseMultipartForm before reaching us, which populates PostForm above.
	if v := r.FormValue("csrf_token"); v != "" {
		return h.sessionMgr.VerifyCSRF(r, v)
	}
	if v := r.URL.Query().Get("csrf_token"); v != "" {
		return h.sessionMgr.VerifyCSRF(r, v)
	}
	return false
}

// failCSRF writes a 403 with a short error message. Used by handlers that
// reject a missing/invalid CSRF token.
func (h *Handler) failCSRF(w http.ResponseWriter) {
	http.Error(w, "Forbidden: missing or invalid CSRF token", http.StatusForbidden)
}

// requireCSRF wraps a handler with a CSRF token check. Suitable for routes
// with URL-encoded bodies — verifyCSRF triggers ParseForm with the default
// 32 MB cap, which is fine for forms but would break large multipart
// uploads. The upload routes call h.verifyCSRF themselves AFTER calling
// ParseMultipartForm with their own size limit.
//
// /api/* routes authenticate via bearer token (Authorization header) and
// are not vulnerable to CSRF, so they don't need this wrapper.
func (h *Handler) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.verifyCSRF(r) {
			h.failCSRF(w)
			return
		}
		next(w, r)
	}
}

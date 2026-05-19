package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cases := []struct {
		name        string
		prefix      string
		method      string
		path        string
		wantHeaders bool
	}{
		{"admin path gets headers", "", "GET", "/admin/projects", true},
		{"login gets headers", "", "GET", "/login", true},
		{"profile gets headers", "", "POST", "/profile/password", true},
		{"project detail gets headers", "", "GET", "/project/foo", true},
		{"upload form gets headers", "", "GET", "/project/foo/upload", true},
		{"upload submit gets headers", "", "POST", "/project/foo/upload", true},
		{"version action gets headers", "", "POST", "/project/foo/version/v1/delete", true},
		{"download gets headers", "", "GET", "/project/foo/version/v1/download", true},
		{"api gets headers", "", "GET", "/api/projects", true},
		{"frontpage gets headers", "", "GET", "/", true},

		{"doc serving exempt", "", "GET", "/project/foo/v1/index.html", false},
		{"doc serving with nested path exempt", "", "GET", "/project/foo/v1/sub/dir/page.html", false},

		{"with base path: admin gets headers", "/docs", "GET", "/docs/admin/projects", true},
		{"with base path: doc serving exempt", "/docs", "GET", "/docs/project/foo/v1/index.html", false},
		{"with base path: version action gets headers", "/docs", "POST", "/docs/project/foo/version/v1/delete", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := SecurityHeadersMiddleware(tc.prefix, next)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			gotNosniff := rr.Header().Get("X-Content-Type-Options")
			gotRef := rr.Header().Get("Referrer-Policy")
			gotCSP := rr.Header().Get("Content-Security-Policy")

			if tc.wantHeaders {
				if gotNosniff != "nosniff" {
					t.Errorf("X-Content-Type-Options = %q, want %q", gotNosniff, "nosniff")
				}
				if gotRef != "strict-origin-when-cross-origin" {
					t.Errorf("Referrer-Policy = %q, want strict-origin-when-cross-origin", gotRef)
				}
				if gotCSP != "frame-ancestors 'self'" {
					t.Errorf("CSP = %q, want frame-ancestors 'self'", gotCSP)
				}
			} else {
				if gotNosniff != "" || gotRef != "" || gotCSP != "" {
					t.Errorf("expected no headers on doc path, got nosniff=%q ref=%q csp=%q",
						gotNosniff, gotRef, gotCSP)
				}
			}
		})
	}
}

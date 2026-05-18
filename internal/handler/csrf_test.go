package handler

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Regression test for M-6: state-changing POSTs are rejected with 403 when
// no CSRF token is present, and accepted with 303 (or appropriate success
// status) when the correct token is supplied. Exercises a representative
// admin endpoint; coverage of every protected route would be redundant
// since they all share the requireCSRF middleware.
func TestPOSTWithoutCSRFTokenRejected(t *testing.T) {
	app := setupTestApp(t)
	hash, _ := auth.HashPassword("admin123")
	app.handler.users.Create(t.Context(), &database.User{
		Username: "csrf-admin", Password: &hash,
		AuthSource: "builtin", Role: "admin",
	})
	cookies := loginUser(t, app, "csrf-admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Same form, two requests: one without token (must 403), one with (must 303).
	build := func(withToken bool) *http.Request {
		form := url.Values{}
		form.Set("slug", "csrf-test")
		form.Set("name", "CSRF test")
		form.Set("visibility", "private")
		if withToken {
			form.Set("csrf_token", csrfTokenFor(t, app, cookies))
		}
		req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects",
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		return req
	}

	resp, err := client.Do(build(false))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("missing CSRF token: expected 403, got %d", resp.StatusCode)
	}

	resp, err = client.Do(build(true))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("with CSRF token: expected 303, got %d", resp.StatusCode)
	}
}

// CSRF token forged with a wrong secret must NOT validate. This is the
// guarantee that an attacker who guesses the session ID still can't forge.
func TestPOSTWithForgedCSRFTokenRejected(t *testing.T) {
	app := setupTestApp(t)
	hash, _ := auth.HashPassword("admin123")
	app.handler.users.Create(t.Context(), &database.User{
		Username: "csrf-admin2", Password: &hash,
		AuthSource: "builtin", Role: "admin",
	})
	cookies := loginUser(t, app, "csrf-admin2", "admin123")

	// Token computed with the wrong secret.
	wrongSecret := make([]byte, 32)
	for i := range wrongSecret {
		wrongSecret[i] = byte(i)
	}
	var sessionID string
	for _, c := range cookies {
		if c.Name == "test_session" {
			sessionID = c.Value
		}
	}
	forged := auth.ComputeCSRFToken(wrongSecret, sessionID)

	form := url.Values{}
	form.Set("slug", "csrf-forged")
	form.Set("name", "forged")
	form.Set("visibility", "private")
	form.Set("csrf_token", forged)

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("forged CSRF token: expected 403, got %d", resp.StatusCode)
	}
}

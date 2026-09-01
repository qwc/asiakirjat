package handler

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// adminPost submits a form to an admin endpoint as the logged-in admin.
func adminPost(t *testing.T, app *testApp, cookies []*http.Cookie, path string, form url.Values) *http.Response {
	t.Helper()

	form.Set("csrf_token", csrfTokenFor(t, app, cookies))
	req, _ := http.NewRequest("POST", app.server.URL+path, strings.NewReader(form.Encode()))
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
	return resp
}

func adminGet(t *testing.T, app *testApp, cookies []*http.Cookie, path string) (int, string) {
	t.Helper()

	req, _ := http.NewRequest("GET", app.server.URL+path, nil)
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
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// The project edit page no longer offers an access-list picker: a project's
// audience is expressed with access grants now, and the list mechanism is kept
// only until its table is retired (#150, #151). What the page must offer
// instead is the exposure choice and a way to grant.
func TestProjectEditPageOffersGrantsNotLists(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	list := &database.AccessList{Name: "writers"}
	if err := app.handler.accessLists.Create(ctx, list); err != nil {
		t.Fatal(err)
	}
	group := &database.AccessGroup{Name: "authors"}
	if err := app.handler.accessGroups.Create(ctx, group); err != nil {
		t.Fatal(err)
	}
	seedProject(t, app, "docs", "Docs", false)

	status, body := adminGet(t, app, cookies, "/admin/projects/docs/edit")
	if status != http.StatusOK {
		t.Fatalf("expected 200 for the edit page, got %d", status)
	}
	if strings.Contains(body, "writers") {
		t.Error("the edit page must no longer offer access lists")
	}
	if !strings.Contains(body, "authors") {
		t.Error("expected the edit page to offer access groups to grant to")
	}
	if !strings.Contains(body, `name="exposure"`) {
		t.Error("expected the edit page to offer the exposure choice")
	}
}

// Groups and organizations decide access across many projects, so managing
// them stays admin-only — even though a project's creator may manage the
// grants on their own project.
func TestNonAdminCannotManageGroupsOrOrgs(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "listeditor", Password: &hash,
		AuthSource: "builtin", Role: "editor",
	}
	if err := app.handler.users.Create(ctx, editor); err != nil {
		t.Fatal(err)
	}
	cookies := loginUser(t, app, "listeditor", "editor123")

	for _, path := range []string{"/admin/access-groups", "/admin/orgs"} {
		if status, _ := adminGet(t, app, cookies, path); status == http.StatusOK {
			t.Errorf("an editor must not see %s", path)
		}
		form := url.Values{}
		form.Set("name", "sneaky")
		if resp := adminPost(t, app, cookies, path, form); resp.StatusCode == http.StatusSeeOther {
			t.Errorf("an editor must not be able to create via %s", path)
		}
	}
}

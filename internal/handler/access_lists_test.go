package handler

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// TestAdminManagesAccessLists walks the page the way an admin would: create a
// list, add an LDAP group and a named user to it, and remove one again.
func TestAdminManagesAccessLists(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	form := url.Values{}
	form.Set("name", "engineering")
	form.Set("description", "Dev team")
	if resp := adminPost(t, app, cookies, "/admin/access-lists", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 after creating a list, got %d", resp.StatusCode)
	}

	list, err := app.handler.accessLists.GetByName(ctx, "engineering")
	if err != nil {
		t.Fatal(err)
	}

	memberPath := "/admin/access-lists/" + strconv.FormatInt(list.ID, 10) + "/members"
	for _, m := range []struct{ typ, id, role string }{
		{"ldap_group", "cn=eng,dc=example,dc=com", "editor"},
		{"user", "alice", "viewer"},
	} {
		form := url.Values{}
		form.Set("subject_type", m.typ)
		form.Set("subject_identifier", m.id)
		form.Set("role", m.role)
		if resp := adminPost(t, app, cookies, memberPath, form); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("expected 303 adding %s, got %d", m.typ, resp.StatusCode)
		}
	}

	members, err := app.handler.accessLists.ListMembers(ctx, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}

	// The page renders both, and the mixed membership is visible.
	status, body := adminGet(t, app, cookies, "/admin/access-lists")
	if status != http.StatusOK {
		t.Fatalf("expected 200 for the access lists page, got %d", status)
	}
	for _, want := range []string{"engineering", "cn=eng,dc=example,dc=com", "alice"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected the page to show %q", want)
		}
	}

	removePath := "/admin/access-lists/members/" + strconv.FormatInt(members[0].ID, 10) + "/delete"
	if resp := adminPost(t, app, cookies, removePath, url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 removing a member, got %d", resp.StatusCode)
	}
	members, _ = app.handler.accessLists.ListMembers(ctx, list.ID)
	if len(members) != 1 {
		t.Errorf("expected 1 member after removal, got %d", len(members))
	}
}

// TestDeletingListInUseIsRefused: the list a project depends on cannot be
// deleted out from under it, and the admin is told why rather than seeing a
// constraint error.
func TestDeletingListInUseIsRefused(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	list := &database.AccessList{Name: "in-use"}
	if err := app.handler.accessLists.Create(ctx, list); err != nil {
		t.Fatal(err)
	}
	project := &database.Project{
		Slug: "guarded", Name: "Guarded",
		Visibility: database.VisibilityList, AccessListID: &list.ID,
	}
	if err := app.handler.projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	deletePath := "/admin/access-lists/" + strconv.FormatInt(list.ID, 10) + "/delete"
	resp := adminPost(t, app, cookies, deletePath, url.Values{})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a redirect carrying the message, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "still+governs") {
		t.Errorf("expected the refusal to explain itself, got %q", loc)
	}

	if _, err := app.handler.accessLists.GetByID(ctx, list.ID); err != nil {
		t.Error("the list should still exist after a refused delete")
	}
}

// TestProjectCanBeGivenListVisibility covers the picker end to end: the edit
// page offers the list, and saving stores the pointer.
func TestProjectCanBeGivenListVisibility(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	list := &database.AccessList{Name: "writers"}
	if err := app.handler.accessLists.Create(ctx, list); err != nil {
		t.Fatal(err)
	}
	project := seedProject(t, app, "docs", "Docs", false)

	status, body := adminGet(t, app, cookies, "/admin/projects/docs/edit")
	if status != http.StatusOK {
		t.Fatalf("expected 200 for the edit page, got %d", status)
	}
	if !strings.Contains(body, "writers") {
		t.Error("expected the edit page to offer the access list")
	}

	form := url.Values{}
	form.Set("slug", project.Slug)
	form.Set("name", project.Name)
	form.Set("visibility", "list")
	form.Set("access_list_id", strconv.FormatInt(list.ID, 10))
	if resp := adminPost(t, app, cookies, "/admin/projects/docs/edit", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 after saving, got %d", resp.StatusCode)
	}

	updated, err := app.handler.projects.GetBySlug(ctx, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Visibility != database.VisibilityList {
		t.Errorf("expected list visibility, got %q", updated.Visibility)
	}
	if updated.AccessListID == nil || *updated.AccessListID != list.ID {
		t.Errorf("expected the list pointer stored, got %v", updated.AccessListID)
	}

	// Switching away clears the pointer rather than leaving it stale.
	form.Set("visibility", "custom")
	if resp := adminPost(t, app, cookies, "/admin/projects/docs/edit", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 after switching away, got %d", resp.StatusCode)
	}
	updated, _ = app.handler.projects.GetBySlug(ctx, "docs")
	if updated.AccessListID != nil {
		t.Errorf("expected the pointer cleared, got %v", *updated.AccessListID)
	}
}

// TestListVisibilityWithoutListIsRejected: the form can't produce a project
// that admits nobody by accident.
func TestListVisibilityWithoutListIsRejected(t *testing.T) {
	app := setupTestApp(t)

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	seedProject(t, app, "nolist", "No List", false)

	form := url.Values{}
	form.Set("slug", "nolist")
	form.Set("name", "No List")
	form.Set("visibility", "list")
	// no access_list_id
	resp := adminPost(t, app, cookies, "/admin/projects/nolist/edit", form)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 without an access list, got %d", resp.StatusCode)
	}
}

// TestNonAdminCannotManageAccessLists — lists govern access across projects,
// so they stay admin-only even though project creators can manage their own
// projects' grants.
func TestNonAdminCannotManageAccessLists(t *testing.T) {
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

	if status, _ := adminGet(t, app, cookies, "/admin/access-lists"); status == http.StatusOK {
		t.Error("an editor must not see the access lists admin page")
	}

	form := url.Values{}
	form.Set("name", "sneaky")
	resp := adminPost(t, app, cookies, "/admin/access-lists", form)
	if resp.StatusCode == http.StatusSeeOther {
		t.Error("an editor must not be able to create an access list")
	}
	if lists, _ := app.handler.accessLists.List(ctx); len(lists) != 0 {
		t.Errorf("expected no lists created, got %d", len(lists))
	}
}

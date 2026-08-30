package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// postRevoke submits the Revoke form from the project edit page as the
// logged-in admin and returns the response status.
func postRevoke(t *testing.T, app *testApp, cookies []*http.Cookie, slug string, form url.Values) int {
	t.Helper()

	form.Set("csrf_token", csrfTokenFor(t, app, cookies))
	req, _ := http.NewRequest("POST",
		app.server.URL+"/admin/projects/"+slug+"/access/revoke",
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

	return resp.StatusCode
}

// TestRevokeRemovesAccessRegardlessOfSource covers issue #126: the project
// edit page lists every project_access row whatever its source, but the
// Revoke button used to delete only source='manual' rows. Revoking an
// LDAP- or OAuth2-granted user silently did nothing: the redirect came
// back 303 and the row was still there, so the button looked dead.
func TestRevokeRemovesAccessRegardlessOfSource(t *testing.T) {
	for _, source := range []string{"manual", "ldap", "oauth2"} {
		t.Run(source, func(t *testing.T) {
			app := setupTestApp(t)
			ctx := context.Background()

			seedAdmin(t, app)
			cookies := loginUser(t, app, "admin", "admin123")

			project := seedProject(t, app, "revoke-"+source, "Revoke "+source, false)

			hash, _ := auth.HashPassword("viewer123")
			viewer := &database.User{
				Username: "viewer-" + source, Password: &hash,
				AuthSource: "builtin", Role: "viewer",
			}
			if err := app.handler.users.Create(ctx, viewer); err != nil {
				t.Fatal(err)
			}

			if err := app.handler.access.Grant(ctx, &database.ProjectAccess{
				ProjectID: project.ID,
				UserID:    viewer.ID,
				Role:      "viewer",
				Source:    source,
			}); err != nil {
				t.Fatal(err)
			}

			form := url.Values{}
			form.Set("user_id", strconv.FormatInt(viewer.ID, 10))
			form.Set("source", source)

			if status := postRevoke(t, app, cookies, project.Slug, form); status != http.StatusSeeOther {
				t.Fatalf("expected 303 after revoke, got %d", status)
			}

			remaining, err := app.handler.access.ListByProject(ctx, project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(remaining) != 0 {
				t.Errorf("expected %s-sourced access to be revoked, still have %d row(s)",
					source, len(remaining))
			}
		})
	}
}

// TestRevokeTargetsTheClickedSource checks that the hidden source field
// actually scopes the delete: revoking with the wrong source must not
// remove a grant that came from somewhere else.
//
// The stronger case — one user holding a manual *and* a synced grant on
// the same project — cannot be set up on SQLite today: migration 002
// never managed to drop the original UNIQUE(project_id, user_id) table
// constraint, so only one row per (project, user) fits. See the
// follow-up issue on that migration.
func TestRevokeTargetsTheClickedSource(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	project := seedProject(t, app, "wrong-source", "Wrong Source", false)

	hash, _ := auth.HashPassword("viewer123")
	viewer := &database.User{
		Username: "ldap-viewer", Password: &hash,
		AuthSource: "ldap", Role: "viewer",
	}
	if err := app.handler.users.Create(ctx, viewer); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID, UserID: viewer.ID, Role: "viewer", Source: "ldap",
	}); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("user_id", strconv.FormatInt(viewer.ID, 10))
	form.Set("source", "manual")

	if status := postRevoke(t, app, cookies, project.Slug, form); status != http.StatusSeeOther {
		t.Fatalf("expected 303 after revoke, got %d", status)
	}

	remaining, err := app.handler.access.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].Source != "ldap" {
		t.Errorf("revoking source=manual must not touch the ldap grant, got %+v", remaining)
	}
}

// TestRevokeRejectsUnknownSource keeps the hidden source field from
// widening into a free-form delete filter.
func TestRevokeRejectsUnknownSource(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	project := seedProject(t, app, "bad-source", "Bad Source", false)

	hash, _ := auth.HashPassword("viewer123")
	viewer := &database.User{
		Username: "bad-source-viewer", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	if err := app.handler.users.Create(ctx, viewer); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID, UserID: viewer.ID, Role: "viewer", Source: "manual",
	}); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("user_id", strconv.FormatInt(viewer.ID, 10))
	form.Set("source", "everything")

	if status := postRevoke(t, app, cookies, project.Slug, form); status != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown source, got %d", status)
	}

	remaining, err := app.handler.access.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Errorf("expected the grant to survive a rejected revoke, got %d row(s)", len(remaining))
	}
}

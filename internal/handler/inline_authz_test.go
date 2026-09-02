package handler

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Regression test for H-1: handleDeleteVersion previously had an inline
// access check that ignored global access grants. A user with a global
// editor grant can upload to a private project but couldn't delete a
// version of it. With the fix (using h.canUpload), they can.
// An org-scoped grant reaches every project in the org. This is what the old
// instance-wide "global access" grant became: the same reach, but expressed at
// a scope an admin can point at, rather than a special case in the checker.
func TestDeleteVersionHonorsOrgGrant(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	// Plain viewer-role user; receives an org-wide editor grant.
	hash, _ := auth.HashPassword("v123")
	viewer := &database.User{
		Username: "h1-grantee", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, viewer)

	grantOrgRole(t, app, defaultOrgID(t, app), viewer.ID, "editor")

	// Need an uploader for the FK on versions.uploaded_by.
	uploader := &database.User{Username: "h1-up", AuthSource: "builtin", Role: "editor"}
	app.handler.users.Create(ctx, uploader)

	priv := &database.Project{Slug: "h1-priv", Name: "H1", Visibility: "private"}
	app.handler.projects.Create(ctx, priv)

	uid := uploader.ID
	v := &database.Version{
		ProjectID:   priv.ID,
		Tag:         "v1",
		StoragePath: "/tmp/notreal",
		ContentType: "archive",
		UploadedBy:  &uid,
	}
	app.handler.versions.Create(ctx, v)

	cookies := loginUser(t, app, "h1-grantee", "v123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/project/h1-priv/version/v1/delete", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	req.Header.Set("X-CSRF-Token", csrfTokenFor(t, app, cookies))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("global-editor grant should permit version delete on private, got %d", resp.StatusCode)
	}
}

// Regression test for H-2: the upload/delete buttons on the project detail
// page must reflect canUpload semantics, not a separate inline rule.
// Specifically, a user with a global editor grant on a private project
// should see CanUpload=true in the rendered page.
// The same cascade, seen through the project page's upload affordance.
func TestProjectDetailCanUploadHonorsOrgGrant(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("v123")
	viewer := &database.User{
		Username: "h2-grantee", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, viewer)

	grantOrgRole(t, app, defaultOrgID(t, app), viewer.ID, "editor")

	priv := &database.Project{Slug: "h2-priv", Name: "H2", Visibility: "private"}
	app.handler.projects.Create(ctx, priv)

	cookies := loginUser(t, app, "h2-grantee", "v123")
	req, _ := http.NewRequest("GET", app.server.URL+"/project/h2-priv", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for permitted viewer, got %d", resp.StatusCode)
	}

	body := make([]byte, 64*1024)
	n, _ := resp.Body.Read(body)
	bodyStr := string(body[:n])
	// The upload-link / button HTML is the most reliable signal that
	// CanUpload was true in the template. Look for the link href that the
	// template renders only when CanUpload is set.
	if !strings.Contains(bodyStr, "/upload") {
		t.Error("expected upload affordance to be rendered for global-editor on private project")
	}
}

// Regression test for H-3: a project-scoped editor token must NOT be able
// to create unrelated projects via POST /api/projects.
func TestAPICreateProjectRejectsProjectScopedToken(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	editor := &database.User{Username: "h3-ed", AuthSource: "robot", Role: "editor", IsRobot: true}
	app.handler.users.Create(ctx, editor)

	// A project to scope the token to.
	scopedProj := &database.Project{Slug: "h3-scoped", Name: "Scoped", Visibility: "private"}
	app.handler.projects.Create(ctx, scopedProj)

	rawToken, _ := auth.GenerateToken(32)
	scopedID := scopedProj.ID
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    editor.ID,
		TokenHash: auth.HashToken(rawToken),
		Name:      "scoped-token",
		ProjectID: &scopedID,
	})

	// Try to create an unrelated project with the scoped token.
	body := bytes.NewBufferString(`{"slug":"h3-attempt","name":"x","visibility":"private"}`)
	req, _ := http.NewRequest("POST", app.server.URL+"/api/projects", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("scoped token must not create unrelated projects; got %d", resp.StatusCode)
	}
	if _, err := app.handler.projects.GetBySlug(ctx, "h3-attempt"); err == nil {
		t.Error("project must not have been created despite the 403")
	}
}

// Sanity: a global (unscoped) editor token can still create projects.
func TestAPICreateProjectAllowsGlobalToken(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	editor := &database.User{Username: "h3-ed-global", AuthSource: "robot", Role: "editor", IsRobot: true}
	app.handler.users.Create(ctx, editor)

	rawToken, _ := auth.GenerateToken(32)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    editor.ID,
		TokenHash: auth.HashToken(rawToken),
		Name:      "global-token",
		// ProjectID nil = global, and creating projects is its own scope (#155)
		Scopes: "upload,create",
	})

	body := bytes.NewBufferString(`{"slug":"h3-ok","name":"x","visibility":"private"}`)
	req, _ := http.NewRequest("POST", app.server.URL+"/api/projects", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("global editor token should create projects, got %d", resp.StatusCode)
	}
}

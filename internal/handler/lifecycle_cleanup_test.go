package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Regression for audit M-1: deleting an LDAP/OAuth2 group mapping must
// also revoke the project_access rows that were granted via that source.
// (Surviving mappings re-grant on next login; this just closes the
// dangling-grant window so a removed mapping doesn't leave access in
// place indefinitely.)
func TestDeleteGroupMappingRevokesProjectAccess(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	adminHash, _ := auth.HashPassword("admin123")
	app.handler.users.Create(ctx, &database.User{
		Username: "m1-admin", Password: &adminHash,
		AuthSource: "builtin", Role: "admin",
	})

	// A user whose access was granted by the mapping we're about to delete.
	grantee := &database.User{Username: "m1-grantee", AuthSource: "oauth2", Role: "viewer"}
	app.handler.users.Create(ctx, grantee)

	project := &database.Project{Slug: "m1-proj", Name: "M1", Visibility: "custom"}
	app.handler.projects.Create(ctx, project)

	mapping := &database.AuthGroupMapping{
		AuthSource: "oauth2", GroupIdentifier: "doomed-group",
		ProjectID: project.ID, Role: "editor",
	}
	app.handler.groupMappings.Create(ctx, mapping)

	// Simulate an existing per-user grant that came from this oauth2 mapping.
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID, UserID: grantee.ID,
		Role: "editor", Source: "oauth2",
	})

	// Delete the mapping.
	cookies := loginUser(t, app, "m1-admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("%s/admin/groups/%d/delete", app.server.URL, mapping.ID), nil)
	req.Header.Set("X-CSRF-Token", csrfTokenFor(t, app, cookies))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Existing oauth2-sourced access on the project must be gone.
	if a, _ := app.handler.access.GetAccessBySource(ctx, project.ID, grantee.ID, "oauth2"); a != nil {
		t.Error("oauth2-sourced access should be revoked after mapping delete")
	}
}

// Regression for audit M-3: changing visibility from public to non-public
// must surface a warning so the admin remembers to review access.
func TestVisibilityRestrictionFlagsFlash(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("admin123")
	app.handler.users.Create(ctx, &database.User{
		Username: "m3-admin", Password: &hash,
		AuthSource: "builtin", Role: "admin",
	})

	// Start as public.
	project := &database.Project{Slug: "m3-proj", Name: "M3", Visibility: "public"}
	app.handler.projects.Create(ctx, project)

	cookies := loginUser(t, app, "m3-admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("slug", "m3-proj")
	form.Set("name", "M3")
	form.Set("visibility", "private")
	form.Set("csrf_token", csrfTokenFor(t, app, cookies))
	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects/m3-proj/edit",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on update, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "visibility_restricted") {
		t.Errorf("expected redirect to carry visibility_restricted flash, got %q", loc)
	}

	// Same edit without a visibility change must NOT carry the flash.
	form2 := url.Values{}
	form2.Set("slug", "m3-proj")
	form2.Set("name", "M3 updated")
	form2.Set("visibility", "private")
	form2.Set("csrf_token", csrfTokenFor(t, app, cookies))
	req2, _ := http.NewRequest("POST", app.server.URL+"/admin/projects/m3-proj/edit",
		strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req2.AddCookie(c)
	}
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if strings.Contains(resp2.Header.Get("Location"), "visibility_restricted") {
		t.Errorf("non-public-origin edit must not flag visibility flash, got %q", resp2.Header.Get("Location"))
	}
}

// Regression for audit M-4: demoting an editor to viewer must revoke
// their manual editor-role ProjectAccess rows and delete their API tokens.
func TestDemotionRevokesManualEditorGrantsAndTokens(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	adminHash, _ := auth.HashPassword("admin123")
	app.handler.users.Create(ctx, &database.User{
		Username: "m4-admin", Password: &adminHash,
		AuthSource: "builtin", Role: "admin",
	})

	editor := &database.User{Username: "m4-editor", AuthSource: "builtin", Role: "editor"}
	app.handler.users.Create(ctx, editor)

	// Editor has: a manual editor grant on a custom project, plus an API token.
	project := &database.Project{Slug: "m4-proj", Name: "M4", Visibility: "custom"}
	app.handler.projects.Create(ctx, project)
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID, UserID: editor.ID,
		Role: "editor", Source: "manual",
	})

	rawToken, _ := auth.GenerateToken(32)
	tok := &database.APIToken{
		UserID: editor.ID, TokenHash: auth.HashToken(rawToken), Name: "soon-revoked",
	}
	app.handler.tokens.Create(ctx, tok)

	// Demote via the admin handler.
	cookies := loginUser(t, app, "m4-admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("role", "viewer")
	form.Set("csrf_token", csrfTokenFor(t, app, cookies))
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("%s/admin/users/%d/role", app.server.URL, editor.ID),
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on demotion, got %d", resp.StatusCode)
	}

	// Manual editor grant should be gone.
	if a, _ := app.handler.access.GetAccessBySource(ctx, project.ID, editor.ID, "manual"); a != nil {
		t.Error("manual editor grant should be revoked after demotion")
	}
	// Token should be gone.
	if _, err := app.handler.tokens.GetByID(ctx, tok.ID); err == nil {
		t.Error("token should be deleted after demotion")
	}
}

// Sanity: demoting admin → admin (no actual change) doesn't touch grants.
func TestPromotionDoesNotRevokeGrants(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	adminHash, _ := auth.HashPassword("admin123")
	app.handler.users.Create(ctx, &database.User{
		Username: "m4-admin2", Password: &adminHash,
		AuthSource: "builtin", Role: "admin",
	})

	viewer := &database.User{Username: "m4-viewer", AuthSource: "builtin", Role: "viewer"}
	app.handler.users.Create(ctx, viewer)

	// Pre-existing manual editor grant (perhaps unusual but possible).
	project := &database.Project{Slug: "m4-proj2", Name: "M4-2", Visibility: "custom"}
	app.handler.projects.Create(ctx, project)
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID, UserID: viewer.ID,
		Role: "editor", Source: "manual",
	})

	cookies := loginUser(t, app, "m4-admin2", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Promote viewer to editor.
	form := url.Values{}
	form.Set("role", "editor")
	form.Set("csrf_token", csrfTokenFor(t, app, cookies))
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("%s/admin/users/%d/role", app.server.URL, viewer.ID),
		bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if a, _ := app.handler.access.GetAccessBySource(ctx, project.ID, viewer.ID, "manual"); a == nil {
		t.Error("promotion (not demotion) must not touch existing grants")
	}
}

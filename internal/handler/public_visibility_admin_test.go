package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Regression test for audit finding C-5: only admins may create public-
// visibility projects. Public projects bypass all access checks
// (search.go canViewProject returns true unconditionally), so handing the
// power to editors is a privilege escalation.

func TestAdminFormEditorCannotCreatePublicProject(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("ed123")
	editor := &database.User{
		Username: "pub-editor", Password: &hash,
		AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, editor)
	cookies := loginUser(t, app, "pub-editor", "ed123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("slug", "ed-pub-attempt")
	form.Set("name", "Editor public attempt")
	form.Set("visibility", "public")

	form.Set("csrf_token", csrfTokenFor(t, app, cookies))
	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("editor must not be allowed to create public project, got %d", resp.StatusCode)
	}
	if _, err := app.handler.projects.GetBySlug(ctx, "ed-pub-attempt"); err == nil {
		t.Error("project must not have been created despite the rejection")
	}
}

func TestAdminFormAdminCanCreatePublicProject(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("ad123")
	admin := &database.User{
		Username: "pub-admin", Password: &hash,
		AuthSource: "builtin", Role: "admin",
	}
	app.handler.users.Create(ctx, admin)
	cookies := loginUser(t, app, "pub-admin", "ad123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("slug", "ad-pub-ok")
	form.Set("name", "Admin public ok")
	form.Set("visibility", "public")

	form.Set("csrf_token", csrfTokenFor(t, app, cookies))
	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects", strings.NewReader(form.Encode()))
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
		t.Errorf("admin must be able to create public project, got %d", resp.StatusCode)
	}
	project, err := app.handler.projects.GetBySlug(ctx, "ad-pub-ok")
	if err != nil {
		t.Fatal("project should exist after admin creation:", err)
	}
	if project.Visibility != "public" {
		t.Errorf("expected visibility public, got %s", project.Visibility)
	}
}

func TestAPICreateProjectEditorCannotCreatePublic(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	editor := &database.User{Username: "pub-api-ed", AuthSource: "robot", Role: "editor", IsRobot: true}
	app.handler.users.Create(ctx, editor)

	rawToken, _ := auth.GenerateToken(32)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: editor.ID, TokenHash: auth.HashToken(rawToken), Name: "t",
	})

	body := bytes.NewBufferString(`{"slug":"api-ed-pub","name":"x","visibility":"public"}`)
	req, _ := http.NewRequest("POST", app.server.URL+"/api/projects", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("editor token must not create public project via API, got %d", resp.StatusCode)
	}
}

func TestAPICreateProjectAdminCanCreatePublic(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	admin := &database.User{Username: "pub-api-ad", AuthSource: "robot", Role: "admin", IsRobot: true}
	app.handler.users.Create(ctx, admin)

	rawToken, _ := auth.GenerateToken(32)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: admin.ID, TokenHash: auth.HashToken(rawToken), Name: "t",
	})

	body := bytes.NewBufferString(`{"slug":"api-ad-pub","name":"x","visibility":"public"}`)
	req, _ := http.NewRequest("POST", app.server.URL+"/api/projects", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("admin token must create public project via API, got %d", resp.StatusCode)
	}
}

package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

func TestEditorCanAccessProjectManagement(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "projecteditor", Password: &hash,
		AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, editor)

	cookies := loginUser(t, app, "projecteditor", "editor123")

	req, _ := http.NewRequest("GET", app.server.URL+"/admin/projects", nil)
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

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for editor accessing project management, got %d", resp.StatusCode)
	}
}

func TestEditorCanCreateProject(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "createeditor", Password: &hash,
		AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, editor)

	cookies := loginUser(t, app, "createeditor", "editor123")

	form := url.Values{}
	form.Set("slug", "editor-created")
	form.Set("name", "Editor Created Project")
	form.Set("description", "Created by an editor")
	form.Set("visibility", "public")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

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
		t.Errorf("expected 303 redirect after editor creates project, got %d", resp.StatusCode)
	}

	// Verify project was created
	project, err := app.handler.projects.GetBySlug(ctx, "editor-created")
	if err != nil {
		t.Fatal("project should exist after editor creation")
	}
	if project.Name != "Editor Created Project" {
		t.Errorf("unexpected project name: %s", project.Name)
	}
}

func TestViewerCannotAccessProjectManagement(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("viewer123")
	viewer := &database.User{
		Username: "projectviewer", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, viewer)

	cookies := loginUser(t, app, "projectviewer", "viewer123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/admin/projects", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for viewer accessing project management, got %d", resp.StatusCode)
	}
}

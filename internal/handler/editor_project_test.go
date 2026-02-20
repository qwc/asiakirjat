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

func TestEditorCreateCustomProjectGetsAccess(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "customeditor", Password: &hash,
		AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, editor)

	cookies := loginUser(t, app, "customeditor", "editor123")

	form := url.Values{}
	form.Set("slug", "custom-proj")
	form.Set("name", "Custom Project")
	form.Set("description", "Custom visibility project")
	form.Set("visibility", "custom")

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
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}

	project, err := app.handler.projects.GetBySlug(ctx, "custom-proj")
	if err != nil {
		t.Fatal("project should exist")
	}

	// Editor should have been auto-granted access
	role, err := app.handler.access.GetEffectiveRole(ctx, project.ID, editor.ID)
	if err != nil {
		t.Fatal("should be able to check access:", err)
	}
	if role != "editor" {
		t.Errorf("editor should have been auto-granted editor access, got role=%q", role)
	}
}

func TestEditorSeesOnlyAccessibleProjects(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	// Create an admin to make projects
	adminHash, _ := auth.HashPassword("admin123")
	admin := &database.User{
		Username: "testadmin", Password: &adminHash,
		AuthSource: "builtin", Role: "admin",
	}
	app.handler.users.Create(ctx, admin)

	// Create an editor
	editorHash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "filtereditor", Password: &editorHash,
		AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, editor)

	// Create a public project (editor should see it)
	pubProject := &database.Project{Slug: "pub-proj", Name: "Public", Visibility: "public"}
	app.handler.projects.Create(ctx, pubProject)

	// Create a custom project without granting editor access (editor should NOT see it)
	customProject := &database.Project{Slug: "hidden-proj", Name: "Hidden", Visibility: "custom"}
	app.handler.projects.Create(ctx, customProject)

	// Create a custom project with editor access (editor should see it)
	accessProject := &database.Project{Slug: "access-proj", Name: "Accessible", Visibility: "custom"}
	app.handler.projects.Create(ctx, accessProject)
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: accessProject.ID,
		UserID:    editor.ID,
		Role:      "viewer",
	})

	cookies := loginUser(t, app, "filtereditor", "editor123")

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
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := make([]byte, 64*1024)
	n, _ := resp.Body.Read(body)
	bodyStr := string(body[:n])

	if !strings.Contains(bodyStr, "Public") {
		t.Error("editor should see public project")
	}
	if strings.Contains(bodyStr, "Hidden") {
		t.Error("editor should NOT see custom project without access")
	}
	if !strings.Contains(bodyStr, "Accessible") {
		t.Error("editor should see custom project with access")
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

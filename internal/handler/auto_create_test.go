package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

func TestIsValidSlug(t *testing.T) {
	tests := []struct {
		slug string
		want bool
	}{
		{"my-project", true},
		{"a", true},
		{"abc123", true},
		{"my-cool-project", true},
		{"a-b-c", true},
		{"project1", true},

		{"", false},
		{"-leading", false},
		{"trailing-", false},
		{"UPPERCASE", false},
		{"has space", false},
		{"has_underscore", false},
		{"a--b", false},
		{"special!char", false},
		{strings.Repeat("a", 129), false},

		{strings.Repeat("a", 128), true},
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			if got := isValidSlug(tt.slug); got != tt.want {
				t.Errorf("isValidSlug(%q) = %v, want %v", tt.slug, got, tt.want)
			}
		})
	}
}

func TestAPIUploadAutoCreateProject(t *testing.T) {
	app := setupTestApp(t)
	app.handler.config.Projects.AutoCreate = true

	ctx := context.Background()

	robot := &database.User{
		Username:   "ci-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    robot.ID,
		TokenHash: tokenHash,
		Name:      "ci-token",
		Scopes:    "upload",
	})

	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html>auto-created project</html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/api/project/new-auto-proj/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Verify project was created
	project, err := app.handler.projects.GetBySlug(ctx, "new-auto-proj")
	if err != nil {
		t.Fatal("expected project to exist after auto-create")
	}
	if project.Visibility != database.VisibilityPrivate {
		t.Errorf("expected private visibility, got %s", project.Visibility)
	}
}

func TestAPIUploadAutoCreateDisabled(t *testing.T) {
	app := setupTestApp(t)
	// auto_create is false by default

	ctx := context.Background()

	robot := &database.User{
		Username:   "ci-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    robot.ID,
		TokenHash: tokenHash,
		Name:      "ci-token",
		Scopes:    "upload",
	})

	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html>test</html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/api/project/nonexistent/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 when auto-create disabled, got %d", resp.StatusCode)
	}
}

func TestAPIUploadAutoCreateViewerDenied(t *testing.T) {
	app := setupTestApp(t)
	app.handler.config.Projects.AutoCreate = true

	ctx := context.Background()

	viewer := &database.User{
		Username:   "viewer-bot",
		AuthSource: "robot",
		Role:       "viewer",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, viewer)

	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    viewer.ID,
		TokenHash: tokenHash,
		Name:      "viewer-token",
		Scopes:    "upload",
	})

	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html>test</html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/api/project/viewer-proj/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for viewer auto-create attempt, got %d", resp.StatusCode)
	}
}

func TestWebUploadAutoCreateProject(t *testing.T) {
	app := setupTestApp(t)
	app.handler.config.Projects.AutoCreate = true

	ctx := context.Background()

	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username:   "editor",
		Email:      "editor@example.com",
		Password:   &hash,
		AuthSource: "builtin",
		Role:       "editor",
	}
	app.handler.users.Create(ctx, editor)

	cookies := loginUser(t, app, "editor", "editor123")

	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html>web auto-created</html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/project/web-auto-proj/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303 redirect, got %d: %s", resp.StatusCode, string(respBody))
	}

	// Verify project was created
	project, err := app.handler.projects.GetBySlug(ctx, "web-auto-proj")
	if err != nil {
		t.Fatal("expected project to exist after web auto-create")
	}
	if project.Visibility != database.VisibilityPrivate {
		t.Errorf("expected private visibility, got %s", project.Visibility)
	}
}

func TestAPICreateProject(t *testing.T) {
	app := setupTestApp(t)

	ctx := context.Background()

	robot := &database.User{
		Username:   "ci-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    robot.ID,
		TokenHash: tokenHash,
		Name:      "ci-token",
		Scopes:    "upload",
	})

	payload := `{"slug":"api-created","name":"API Created Project","description":"Created via API"}`
	req, _ := http.NewRequest("POST", app.server.URL+"/api/projects", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["slug"] != "api-created" {
		t.Errorf("expected slug api-created, got %s", result["slug"])
	}
	if result["visibility"] != "private" {
		t.Errorf("expected default visibility private, got %s", result["visibility"])
	}

	// Verify project exists in DB
	project, err := app.handler.projects.GetBySlug(ctx, "api-created")
	if err != nil {
		t.Fatal("expected project to exist")
	}
	if project.Name != "API Created Project" {
		t.Errorf("expected name 'API Created Project', got %s", project.Name)
	}
}

func TestAPICreateProjectDuplicate(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "existing-proj", "Existing", false)

	ctx := context.Background()

	robot := &database.User{
		Username:   "ci-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    robot.ID,
		TokenHash: tokenHash,
		Name:      "ci-token",
		Scopes:    "upload",
	})

	payload := `{"slug":"existing-proj"}`
	req, _ := http.NewRequest("POST", app.server.URL+"/api/projects", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 for duplicate, got %d", resp.StatusCode)
	}
}

func TestAPICreateProjectViewerDenied(t *testing.T) {
	app := setupTestApp(t)

	ctx := context.Background()

	viewer := &database.User{
		Username:   "viewer-bot",
		AuthSource: "robot",
		Role:       "viewer",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, viewer)

	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    viewer.ID,
		TokenHash: tokenHash,
		Name:      "viewer-token",
		Scopes:    "upload",
	})

	payload := `{"slug":"viewer-proj"}`
	req, _ := http.NewRequest("POST", app.server.URL+"/api/projects", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for viewer, got %d", resp.StatusCode)
	}
}

func TestAPICreateProjectInvalidSlug(t *testing.T) {
	app := setupTestApp(t)

	ctx := context.Background()

	robot := &database.User{
		Username:   "ci-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    robot.ID,
		TokenHash: tokenHash,
		Name:      "ci-token",
		Scopes:    "upload",
	})

	payload := `{"slug":"INVALID SLUG!"}`
	req, _ := http.NewRequest("POST", app.server.URL+"/api/projects", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid slug, got %d", resp.StatusCode)
	}
}

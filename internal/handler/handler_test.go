package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/templates"
	"github.com/qwc/asiakirjat/internal/testutil"
)

type testApp struct {
	handler *Handler
	mux     *http.ServeMux
	server  *httptest.Server
	db      interface{}
}

func setupTestApp(t *testing.T) *testApp {
	t.Helper()

	db := testutil.NewTestDB(t)
	storageDir := t.TempDir()

	cfg := config.Defaults()
	cfg.Storage.BasePath = storageDir

	projectStore := sqlstore.NewProjectStore(db)
	versionStore := sqlstore.NewVersionStore(db)
	userStore := sqlstore.NewUserStore(db)
	sessionStore := sqlstore.NewSessionStore(db)
	accessStore := sqlstore.NewProjectAccessStore(db)
	tokenStore := sqlstore.NewTokenStore(db)

	storage := docs.NewFilesystemStorage(storageDir)

	searchIndex, err := docs.NewSearchIndex(storageDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { searchIndex.Close() })

	sessionMgr := auth.NewSessionManager(sessionStore, userStore, "test_session", 86400, false)
	builtinAuth := auth.NewBuiltinAuthenticator(userStore)

	tmpl, err := templates.New()
	if err != nil {
		t.Fatal(err)
	}

	// Create a minimal static FS for testing
	staticDir := t.TempDir()
	os.MkdirAll(filepath.Join(staticDir, "css"), 0755)
	os.MkdirAll(filepath.Join(staticDir, "js"), 0755)
	os.WriteFile(filepath.Join(staticDir, "css", "style.css"), []byte("/* test */"), 0644)
	os.WriteFile(filepath.Join(staticDir, "js", "search.js"), []byte("// test"), 0644)
	os.WriteFile(filepath.Join(staticDir, "js", "overlay.js"), []byte("// test"), 0644)
	os.WriteFile(filepath.Join(staticDir, "js", "navbar-search.js"), []byte("// test"), 0644)
	staticFS := os.DirFS(staticDir)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	h := New(Deps{
		Config:         &cfg,
		Templates:      tmpl,
		Storage:        storage,
		StaticFS:       staticFS,
		Projects:       projectStore,
		Versions:       versionStore,
		Users:          userStore,
		Sessions:       sessionStore,
		Access:         accessStore,
		Tokens:         tokenStore,
		Authenticators: []auth.Authenticator{builtinAuth},
		SessionMgr:     sessionMgr,
		SearchIndex:    searchIndex,
		Logger:         logger,
	})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return &testApp{handler: h, mux: mux, server: server, db: db}
}

func seedAdmin(t *testing.T, app *testApp) *database.User {
	t.Helper()
	ctx := context.Background()
	hash, _ := auth.HashPassword("admin123")
	user := &database.User{
		Username:   "admin",
		Email:      "admin@example.com",
		Password:   &hash,
		AuthSource: "builtin",
		Role:       "admin",
	}
	if err := app.handler.users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	return user
}

func seedProject(t *testing.T, app *testApp, slug, name string, isPublic bool) *database.Project {
	t.Helper()
	ctx := context.Background()
	visibility := database.VisibilityCustom
	if isPublic {
		visibility = database.VisibilityPublic
	}
	project := &database.Project{
		Slug:       slug,
		Name:       name,
		Visibility: visibility,
	}
	if err := app.handler.projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	return project
}

func loginUser(t *testing.T, app *testApp, username, password string) []*http.Cookie {
	t.Helper()
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.PostForm(app.server.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	return resp.Cookies()
}

func TestFrontpagePublic(t *testing.T) {
	app := setupTestApp(t)

	// Create a public project
	seedProject(t, app, "public-proj", "Public Project", true)

	resp, err := http.Get(app.server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestFrontpageShowsPublicProjects(t *testing.T) {
	app := setupTestApp(t)

	seedProject(t, app, "public-proj", "Public Project", true)
	seedProject(t, app, "private-proj", "Private Project", false)

	resp, err := http.Get(app.server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "Public Project") {
		t.Error("expected public project to be visible")
	}
	if strings.Contains(body, "Private Project") {
		t.Error("expected private project to be hidden from anonymous users")
	}
}

func TestLoginLogout(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	// Login
	cookies := loginUser(t, app, "admin", "admin123")
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after login")
	}

	// Access frontpage with session
	req, _ := http.NewRequest("GET", app.server.URL+"/", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verify user is shown in navbar
	loginBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(loginBody), "admin") {
		t.Error("expected username 'admin' in response")
	}
}

func TestProtectedRouteRedirectsToLogin(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "test-proj", "Test", true)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(app.server.URL + "/project/test-proj/upload")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc != "/login" {
		t.Errorf("expected redirect to /login, got %s", loc)
	}
}

func TestProjectDetailNotFound(t *testing.T) {
	app := setupTestApp(t)

	resp, err := http.Get(app.server.URL + "/project/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestProjectDetailPublic(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "docs", "Documentation", true)

	resp, err := http.Get(app.server.URL + "/project/docs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPrivateProjectRedirectsAnonymous(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "secret", "Secret Project", false)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(app.server.URL + "/project/secret")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", resp.StatusCode)
	}
}

func TestDocServing(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Docs", true)

	// Create version and files on disk
	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("docs", "v1.0.0")
	versionPath := storage.VersionPath("docs", "v1.0.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"), []byte("<html>hello world</html>"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	resp, err := http.Get(app.server.URL + "/project/docs/v1.0.0/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	docBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(docBody), "hello world") {
		t.Error("expected doc content in response")
	}
}

func TestDocServingVersionNotFound(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "docs", "Docs", true)

	resp, err := http.Get(app.server.URL + "/project/docs/v99.0.0/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	app := setupTestApp(t)

	resp, err := http.Get(app.server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAPIProjects(t *testing.T) {
	app := setupTestApp(t)

	seedProject(t, app, "proj-a", "Project A", true)
	seedProject(t, app, "proj-b", "Project B", true)

	resp, err := http.Get(app.server.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON content type, got %s", ct)
	}
}

func TestAPIProjectSearch(t *testing.T) {
	app := setupTestApp(t)

	seedProject(t, app, "golang-docs", "Go Documentation", true)
	seedProject(t, app, "python-docs", "Python Docs", true)

	resp, err := http.Get(app.server.URL + "/api/projects?q=golang")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "golang-docs") {
		t.Error("expected golang-docs in search results")
	}
	if strings.Contains(body, "python-docs") {
		t.Error("expected python-docs NOT in search results")
	}
}

func TestAPIVersions(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "proj", "Project", true)

	ctx := context.Background()
	app.handler.versions.Create(ctx, &database.Version{
		ProjectID: project.ID, Tag: "v1.0.0",
		StoragePath: "/data/v1.0.0", UploadedBy: admin.ID,
	})
	app.handler.versions.Create(ctx, &database.Version{
		ProjectID: project.ID, Tag: "v2.0.0",
		StoragePath: "/data/v2.0.0", UploadedBy: admin.ID,
	})

	resp, err := http.Get(app.server.URL + "/api/project/proj/versions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	if !strings.Contains(body, "v1.0.0") || !strings.Contains(body, "v2.0.0") {
		t.Error("expected both versions in response")
	}
}

func TestLoginWithInvalidPassword(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "wrongpassword")
	resp, err := client.PostForm(app.server.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should re-render login page (200), not redirect
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (re-rendered login), got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	if !strings.Contains(body, "Invalid username or password") {
		t.Error("expected error message in response")
	}
}

func TestLoginWithEmptyFields(t *testing.T) {
	app := setupTestApp(t)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("username", "")
	form.Set("password", "")
	resp, err := client.PostForm(app.server.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (re-rendered login), got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	if !strings.Contains(body, "Username and password are required") {
		t.Error("expected validation error message in response")
	}
}

func TestLogoutClearsSession(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	// Login
	cookies := loginUser(t, app, "admin", "admin123")
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after login")
	}

	// Logout
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest("GET", app.server.URL+"/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after logout, got %d", resp.StatusCode)
	}

	// Try to access a protected route with the old cookie — should redirect to login
	req2, _ := http.NewRequest("GET", app.server.URL+"/project/test-proj/upload", nil)
	for _, c := range cookies {
		req2.AddCookie(c)
	}
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after logout, got %d", resp2.StatusCode)
	}
}

func TestRobotUserCannotLogin(t *testing.T) {
	app := setupTestApp(t)

	// Create a robot user with a password
	ctx := context.Background()
	hash, _ := auth.HashPassword("robotpass")
	robot := &database.User{
		Username:   "ci-bot",
		Password:   &hash,
		AuthSource: "builtin",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("username", "ci-bot")
	form.Set("password", "robotpass")
	resp, err := client.PostForm(app.server.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should fail login (200 with error message, not 303 redirect)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (re-rendered login), got %d", resp.StatusCode)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	if !strings.Contains(body, "Invalid username or password") {
		t.Error("expected error message, robot should not be able to log in")
	}
}

func TestAdminRequiredReturns403(t *testing.T) {
	app := setupTestApp(t)

	// Create a non-admin user
	ctx := context.Background()
	hash, _ := auth.HashPassword("viewer123")
	viewer := &database.User{
		Username:   "viewer",
		Password:   &hash,
		AuthSource: "builtin",
		Role:       "viewer",
	}
	app.handler.users.Create(ctx, viewer)

	cookies := loginUser(t, app, "viewer", "viewer123")
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after login")
	}

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
		t.Errorf("expected 403 for non-admin user, got %d", resp.StatusCode)
	}
}

func TestLoginPageRedirectsAuthenticatedUser(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/login", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for already-logged-in user visiting /login, got %d", resp.StatusCode)
	}
}

func createTestZip(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	w.Close()
	return buf
}

func TestUploadFormRequiresAuth(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "proj", "Project", true)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(app.server.URL + "/project/proj/upload")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect to login, got %d", resp.StatusCode)
	}
}

func TestUploadFullFlow(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	_ = admin
	seedProject(t, app, "docs", "Documentation", true)

	cookies := loginUser(t, app, "admin", "admin123")
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after login")
	}

	// Create a test zip
	zipBuf := createTestZip(t, map[string]string{
		"index.html":        "<html><body>Hello docs!</body></html>",
		"css/style.css":     "body { color: blue; }",
		"guide/intro.html":  "<html><body>Introduction</body></html>",
	})

	// Build multipart form
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, err := writer.CreateFormFile("archive", "docs.zip")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(zipBuf.Bytes())
	writer.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should redirect to project detail page
	if resp.StatusCode != http.StatusSeeOther {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303 redirect after upload, got %d: %s", resp.StatusCode, string(bodyBytes))
	}

	loc := resp.Header.Get("Location")
	if loc != "/project/docs" {
		t.Errorf("expected redirect to /project/docs, got %s", loc)
	}

	// Verify the version was created — check via API
	apiResp, err := http.Get(app.server.URL + "/api/project/docs/versions")
	if err != nil {
		t.Fatal(err)
	}
	defer apiResp.Body.Close()

	apiBody, _ := io.ReadAll(apiResp.Body)
	if !strings.Contains(string(apiBody), "v1.0.0") {
		t.Error("expected v1.0.0 in version list after upload")
	}

	// Verify the actual doc files are served
	docResp, err := http.Get(app.server.URL + "/project/docs/v1.0.0/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer docResp.Body.Close()

	if docResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for uploaded doc, got %d", docResp.StatusCode)
	}

	docBody, _ := io.ReadAll(docResp.Body)
	if !strings.Contains(string(docBody), "Hello docs!") {
		t.Error("expected uploaded content in served doc")
	}

	// Verify nested files
	cssResp, err := http.Get(app.server.URL + "/project/docs/v1.0.0/css/style.css")
	if err != nil {
		t.Fatal(err)
	}
	defer cssResp.Body.Close()

	if cssResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for nested CSS file, got %d", cssResp.StatusCode)
	}
}

func TestUploadVersionReupload(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "proj", "Project", true)

	// Create existing version
	ctx := context.Background()
	app.handler.storage.EnsureVersionDir("proj", "v1.0.0")
	app.handler.versions.Create(ctx, &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: app.handler.storage.VersionPath("proj", "v1.0.0"),
		UploadedBy:  admin.ID,
	})

	cookies := loginUser(t, app, "admin", "admin123")

	zipBuf := createTestZip(t, map[string]string{"index.html": "new content"})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/project/proj/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
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

	// Re-upload should succeed and redirect
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after re-upload, got %d", resp.StatusCode)
	}

	// Verify version still exists with same ID
	version, err := app.handler.versions.GetByProjectAndTag(ctx, project.ID, "v1.0.0")
	if err != nil {
		t.Fatalf("version should still exist: %v", err)
	}
	if version == nil {
		t.Fatal("version should not be nil")
	}
}

func TestUploadMissingVersion(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	seedProject(t, app, "proj", "Project", true)

	cookies := loginUser(t, app, "admin", "admin123")

	zipBuf := createTestZip(t, map[string]string{"index.html": "test"})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	// Intentionally omit version field
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/project/proj/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
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
		t.Errorf("expected 200 (upload page with error), got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), "Version tag is required") {
		t.Error("expected version tag required error message")
	}
}

func TestViewerCannotUpload(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "proj", "Project", true)

	// Create viewer user
	ctx := context.Background()
	hash, _ := auth.HashPassword("viewer123")
	app.handler.users.Create(ctx, &database.User{
		Username: "viewer", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	})

	cookies := loginUser(t, app, "viewer", "viewer123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/project/proj/upload", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for viewer on upload page, got %d", resp.StatusCode)
	}
}

func TestAdminCreateProject(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("slug", "new-project")
	form.Set("name", "New Project")
	form.Set("description", "A test project")
	form.Set("visibility", "public")

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

	// Verify project was created
	ctx := context.Background()
	project, err := app.handler.projects.GetBySlug(ctx, "new-project")
	if err != nil {
		t.Fatal("expected project to be created")
	}
	if project.Name != "New Project" {
		t.Errorf("expected project name 'New Project', got %q", project.Name)
	}
	if project.Visibility != database.VisibilityPublic {
		t.Errorf("expected project visibility 'public', got %q", project.Visibility)
	}
}

func TestAdminUpdateProject(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	seedProject(t, app, "update-me", "Original Name", false)
	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("slug", "update-me")
	form.Set("name", "Updated Name")
	form.Set("description", "Updated description")
	form.Set("visibility", "public")

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects/update-me/edit", strings.NewReader(form.Encode()))
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

	ctx := context.Background()
	project, _ := app.handler.projects.GetBySlug(ctx, "update-me")
	if project.Name != "Updated Name" {
		t.Errorf("expected 'Updated Name', got %q", project.Name)
	}
	if project.Visibility != database.VisibilityPublic {
		t.Errorf("expected project visibility 'public', got %q", project.Visibility)
	}
}

func TestAdminDeleteProject(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	seedProject(t, app, "delete-me", "Delete Me", true)
	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects/delete-me/delete", nil)
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

	ctx := context.Background()
	_, err = app.handler.projects.GetBySlug(ctx, "delete-me")
	if err == nil {
		t.Error("expected project to be deleted")
	}
}

func TestAdminCreateUser(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("username", "neweditor")
	form.Set("password", "pass123")
	form.Set("email", "editor@example.com")
	form.Set("role", "editor")

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/users", strings.NewReader(form.Encode()))
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

	ctx := context.Background()
	user, err := app.handler.users.GetByUsername(ctx, "neweditor")
	if err != nil {
		t.Fatal("expected user to be created")
	}
	if user.Role != "editor" {
		t.Errorf("expected role 'editor', got %q", user.Role)
	}
}

func TestAdminDeleteUser(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	// Create a user to delete
	ctx := context.Background()
	hash, _ := auth.HashPassword("pass")
	deleteMe := &database.User{
		Username: "deleteme", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, deleteMe)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/users/"+strings.TrimSpace(fmt.Sprintf("%d", deleteMe.ID))+"/delete", nil)
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

	_, err = app.handler.users.GetByUsername(ctx, "deleteme")
	if err == nil {
		t.Error("expected user to be deleted")
	}
}

func TestEditorCanUploadToAssignedProject(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "proj", "Project", true)

	// Create an editor user
	ctx := context.Background()
	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "editor", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, editor)

	// Grant editor access to the project
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    editor.ID,
		Role:      "editor",
	})

	cookies := loginUser(t, app, "editor", "editor123")

	// Check editor can see upload page
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/project/proj/upload", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for editor with access, got %d", resp.StatusCode)
	}
}

func TestPrivateProjectAccessForGrantedUser(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "private-proj", "Private", false)

	ctx := context.Background()
	hash, _ := auth.HashPassword("user123")
	user := &database.User{
		Username: "granteduser", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, user)

	// Grant access
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    user.ID,
		Role:      "viewer",
	})

	cookies := loginUser(t, app, "granteduser", "user123")

	req, _ := http.NewRequest("GET", app.server.URL+"/project/private-proj", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for user with access, got %d", resp.StatusCode)
	}
}

func TestPrivateProjectForbiddenWithoutAccess(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	seedProject(t, app, "secret", "Secret", false)

	ctx := context.Background()
	hash, _ := auth.HashPassword("user123")
	user := &database.User{
		Username: "noaccess", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "noaccess", "user123")

	req, _ := http.NewRequest("GET", app.server.URL+"/project/secret", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for user without access, got %d", resp.StatusCode)
	}
}

func TestAPIUploadWithBearerToken(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "api-proj", "API Project", true)

	ctx := context.Background()

	// Create robot user
	robot := &database.User{
		Username:   "ci-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	// Grant upload access
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    robot.ID,
		Role:      "editor",
	})

	// Generate token
	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    robot.ID,
		TokenHash: tokenHash,
		Name:      "ci-token",
		Scopes:    "upload",
	})

	// Build multipart upload
	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html>API uploaded</html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v2.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/api/project/api-proj/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), "v2.0.0") {
		t.Error("expected version in response")
	}

	// Verify docs are served
	docResp, _ := http.Get(app.server.URL + "/project/api-proj/v2.0.0/index.html")
	defer docResp.Body.Close()

	if docResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for API-uploaded doc, got %d", docResp.StatusCode)
	}

	docBody, _ := io.ReadAll(docResp.Body)
	if !strings.Contains(string(docBody), "API uploaded") {
		t.Error("expected uploaded content")
	}
}

func TestAPIUploadUnauthorized(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "proj", "Project", true)

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(createTestZip(t, map[string]string{"index.html": "test"}).Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/api/project/proj/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	// No Authorization header

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

func TestAPIUploadInvalidToken(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "proj", "Project", true)

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(createTestZip(t, map[string]string{"index.html": "test"}).Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/api/project/proj/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer invalidtoken123")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token, got %d", resp.StatusCode)
	}
}

func TestAPIUploadWithoutProjectAccess(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	seedProject(t, app, "restricted", "Restricted", false)

	ctx := context.Background()

	// Create robot with NO access to the project
	robot := &database.User{
		Username:   "no-access-bot",
		AuthSource: "robot",
		Role:       "viewer", // viewer role, no project access
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    robot.ID,
		TokenHash: tokenHash,
		Name:      "limited-token",
		Scopes:    "upload",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(createTestZip(t, map[string]string{"index.html": "test"}).Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/api/project/restricted/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 without project access, got %d", resp.StatusCode)
	}
}

func TestAdminCreateRobotAndGenerateToken(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Create robot
	form := url.Values{}
	form.Set("username", "deploy-bot")

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/robots", strings.NewReader(form.Encode()))
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
		t.Errorf("expected 303 after creating robot, got %d", resp.StatusCode)
	}

	// Verify robot was created
	ctx := context.Background()
	robot, err := app.handler.users.GetByUsername(ctx, "deploy-bot")
	if err != nil {
		t.Fatal("expected robot to be created")
	}
	if !robot.IsRobot {
		t.Error("expected user to be a robot")
	}
	if robot.AuthSource != "robot" {
		t.Errorf("expected auth_source 'robot', got %q", robot.AuthSource)
	}

	// Generate token for the robot
	form2 := url.Values{}
	form2.Set("name", "deploy-token")

	req2, _ := http.NewRequest("POST", app.server.URL+fmt.Sprintf("/admin/robots/%d/tokens", robot.ID), strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req2.AddCookie(c)
	}

	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	// Should render page with new token (200, not redirect)
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with new token displayed, got %d", resp2.StatusCode)
	}

	respBody, _ := io.ReadAll(resp2.Body)
	body2 := string(respBody)
	if !strings.Contains(body2, "New API Token Generated") {
		t.Error("expected token display message in response")
	}
}

func TestOverlayInjectedInHTMLDoc(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "overlay-test", "Overlay Test", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("overlay-test", "v1.0.0")
	versionPath := storage.VersionPath("overlay-test", "v1.0.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"),
		[]byte("<html><head><title>Test</title></head><body><h1>Hello</h1></body></html>"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	resp, err := http.Get(app.server.URL + "/project/overlay-test/v1.0.0/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Original content preserved
	if !strings.Contains(bodyStr, "<h1>Hello</h1>") {
		t.Error("expected original content preserved")
	}

	// Overlay should be injected
	if !strings.Contains(bodyStr, "asiakirjat-overlay") {
		t.Error("expected overlay div in response")
	}

	// Overlay should contain project name and version
	if !strings.Contains(bodyStr, "Overlay Test") {
		t.Error("expected project name in overlay")
	}
	if !strings.Contains(bodyStr, "v1.0.0") {
		t.Error("expected version in overlay")
	}

	// Overlay should contain script reference
	if !strings.Contains(bodyStr, "overlay.js") {
		t.Error("expected overlay.js script tag")
	}

	// Overlay should appear before </body>
	overlayIdx := strings.Index(bodyStr, "asiakirjat-overlay")
	bodyCloseIdx := strings.Index(strings.ToLower(bodyStr), "</body>")
	if overlayIdx == -1 || bodyCloseIdx == -1 {
		t.Fatal("could not find overlay or </body> in response")
	}
	if overlayIdx > bodyCloseIdx {
		t.Error("overlay should be injected before </body>")
	}
}

func TestOverlayNotInjectedInCSS(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "css-test", "CSS Test", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("css-test", "v1.0.0")
	versionPath := storage.VersionPath("css-test", "v1.0.0")
	os.WriteFile(filepath.Join(versionPath, "style.css"),
		[]byte("body { color: red; }"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	resp, err := http.Get(app.server.URL + "/project/css-test/v1.0.0/style.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if strings.Contains(bodyStr, "asiakirjat-overlay") {
		t.Error("overlay should NOT be injected into CSS files")
	}
	if !strings.Contains(bodyStr, "body { color: red; }") {
		t.Error("expected original CSS content")
	}
}

func TestOverlayNotInjectedInImage(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "img-test", "Image Test", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("img-test", "v1.0.0")
	versionPath := storage.VersionPath("img-test", "v1.0.0")
	// Write a fake PNG file
	os.WriteFile(filepath.Join(versionPath, "logo.png"),
		[]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	resp, err := http.Get(app.server.URL + "/project/img-test/v1.0.0/logo.png")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) != 8 {
		t.Errorf("expected 8 bytes for PNG, got %d", len(body))
	}
}

func TestOverlayInjectedInDirectoryIndex(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "dir-test", "Dir Test", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("dir-test", "v2.0")
	versionPath := storage.VersionPath("dir-test", "v2.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"),
		[]byte("<html><body><p>Root index</p></body></html>"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v2.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	// Request the root with empty path (directory index)
	resp, err := http.Get(app.server.URL + "/project/dir-test/v2.0/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "Root index") {
		t.Error("expected original content")
	}
	if !strings.Contains(bodyStr, "asiakirjat-overlay") {
		t.Error("expected overlay in directory index response")
	}
}

func TestAPIVersionsSemverSorted(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "ver-sort", "Version Sort", true)

	ctx := context.Background()
	storage := app.handler.storage

	// Create versions in non-sorted order
	for _, tag := range []string{"v1.0.0", "v2.0.0", "v1.5.0", "v1.10.0"} {
		storage.EnsureVersionDir("ver-sort", tag)
		vp := storage.VersionPath("ver-sort", tag)
		app.handler.versions.Create(ctx, &database.Version{
			ProjectID:   project.ID,
			Tag:         tag,
			StoragePath: vp,
			UploadedBy:  admin.ID,
		})
	}

	resp, err := http.Get(app.server.URL + "/api/project/ver-sort/versions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Check semver ordering: v2.0.0 should appear before v1.10.0, which should appear before v1.5.0, etc.
	idx200 := strings.Index(bodyStr, "v2.0.0")
	idx1100 := strings.Index(bodyStr, "v1.10.0")
	idx150 := strings.Index(bodyStr, "v1.5.0")
	idx100 := strings.Index(bodyStr, "v1.0.0")

	if idx200 == -1 || idx1100 == -1 || idx150 == -1 || idx100 == -1 {
		t.Fatalf("expected all versions in response, got: %s", bodyStr)
	}

	if idx200 > idx1100 {
		t.Error("v2.0.0 should appear before v1.10.0")
	}
	if idx1100 > idx150 {
		t.Error("v1.10.0 should appear before v1.5.0")
	}
	if idx150 > idx100 {
		t.Error("v1.5.0 should appear before v1.0.0")
	}

	// Should contain created_at fields
	if !strings.Contains(bodyStr, "created_at") {
		t.Error("expected created_at in version response")
	}
}

func TestAPIProjectSearchFilters(t *testing.T) {
	app := setupTestApp(t)

	seedProject(t, app, "golang-docs", "Go Documentation", true)
	seedProject(t, app, "python-docs", "Python Documentation", true)
	seedProject(t, app, "rust-manual", "Rust Manual", true)

	// Search for "golang" should match only the Go project
	resp, err := http.Get(app.server.URL + "/api/projects?q=golang")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "golang-docs") {
		t.Error("expected golang-docs in search results")
	}
	if strings.Contains(bodyStr, "python-docs") {
		t.Error("python-docs should not appear in golang search")
	}
	if strings.Contains(bodyStr, "rust-manual") {
		t.Error("rust-manual should not appear in golang search")
	}
}

func TestAPIProjectSearchByName(t *testing.T) {
	app := setupTestApp(t)

	seedProject(t, app, "mylib", "My Library", true)
	seedProject(t, app, "other", "Other Tool", true)

	// Search by project name
	resp, err := http.Get(app.server.URL + "/api/projects?q=Library")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "mylib") {
		t.Error("expected mylib in name search results")
	}
	if strings.Contains(bodyStr, "other") {
		t.Error("other should not appear in Library search")
	}
}

func TestAPIProjectSearchNoQuery(t *testing.T) {
	app := setupTestApp(t)

	seedProject(t, app, "proj-a", "Project A", true)
	seedProject(t, app, "proj-b", "Project B", true)

	// No query should return all accessible projects
	resp, err := http.Get(app.server.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "proj-a") {
		t.Error("expected proj-a in results")
	}
	if !strings.Contains(bodyStr, "proj-b") {
		t.Error("expected proj-b in results")
	}
}

func TestProjectDetailMarkdownDescription(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := &database.Project{
		Slug:        "md-test",
		Name:        "Markdown Test",
		Description: "This is **bold** and *italic* text.\n\n- Item 1\n- Item 2",
		Visibility:  database.VisibilityPublic,
	}
	app.handler.projects.Create(ctx, project)

	resp, err := http.Get(app.server.URL + "/project/md-test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Should contain rendered markdown HTML
	if !strings.Contains(bodyStr, "<strong>bold</strong>") {
		t.Error("expected markdown bold rendering")
	}
	if !strings.Contains(bodyStr, "<em>italic</em>") {
		t.Error("expected markdown italic rendering")
	}
	if !strings.Contains(bodyStr, "<li>Item 1</li>") {
		t.Error("expected markdown list rendering")
	}
}

func TestProjectDetailSemverSortedVersions(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "sorted-proj", "Sorted Project", true)

	ctx := context.Background()
	storage := app.handler.storage

	for _, tag := range []string{"v1.0.0", "v3.0.0", "v2.0.0"} {
		storage.EnsureVersionDir("sorted-proj", tag)
		vp := storage.VersionPath("sorted-proj", tag)
		app.handler.versions.Create(ctx, &database.Version{
			ProjectID:   project.ID,
			Tag:         tag,
			StoragePath: vp,
			UploadedBy:  admin.ID,
		})
	}

	resp, err := http.Get(app.server.URL + "/project/sorted-proj")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// v3.0.0 should appear before v2.0.0 and v1.0.0
	idx300 := strings.Index(bodyStr, "v3.0.0")
	idx200 := strings.Index(bodyStr, "v2.0.0")
	idx100 := strings.Index(bodyStr, "v1.0.0")

	if idx300 == -1 || idx200 == -1 || idx100 == -1 {
		t.Fatal("expected all versions in project detail page")
	}

	if idx300 > idx200 {
		t.Error("v3.0.0 should appear before v2.0.0")
	}
	if idx200 > idx100 {
		t.Error("v2.0.0 should appear before v1.0.0")
	}
}

func TestLoginRateLimiting(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	// Set a low limit for testing
	app.handler.loginLimiter = NewRateLimiter(3, time.Minute)

	// Re-register routes with the new limiter
	mux := http.NewServeMux()
	app.handler.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Make 3 login attempts with X-Forwarded-For (all allowed)
	for i := range 3 {
		form := url.Values{}
		form.Set("username", "admin")
		form.Set("password", "wrongpass")

		req, _ := http.NewRequest("POST", server.URL+"/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", "10.0.0.99")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			t.Errorf("request %d should not be rate limited", i+1)
		}
	}

	// 4th attempt should be rate limited
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "wrongpass")

	req, _ := http.NewRequest("POST", server.URL+"/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", "10.0.0.99")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 after rate limit exceeded, got %d", resp.StatusCode)
	}
}

func TestAdminResetPasswordBuiltinUser(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	// Create a builtin user to reset
	ctx := context.Background()
	hash, _ := auth.HashPassword("oldpass")
	target := &database.User{
		Username: "resetme", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, target)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("password", "newpass123")

	req, _ := http.NewRequest("POST", app.server.URL+fmt.Sprintf("/admin/users/%d/password", target.ID), strings.NewReader(form.Encode()))
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

	// Verify login with new password works
	newCookies := loginUser(t, app, "resetme", "newpass123")
	if len(newCookies) == 0 {
		t.Error("expected login to succeed with new password after admin reset")
	}

	// Verify old password no longer works
	oldCookies := loginUser(t, app, "resetme", "oldpass")
	hasSession := false
	for _, c := range oldCookies {
		if c.Name == "test_session" {
			hasSession = true
		}
	}
	if hasSession {
		t.Error("expected old password to no longer work")
	}
}

func TestAdminResetPasswordRejectedForNonBuiltin(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	// Create a non-builtin (LDAP) user
	ctx := context.Background()
	ldapUser := &database.User{
		Username:   "ldapuser",
		Email:      "ldap@example.com",
		AuthSource: "ldap",
		Role:       "viewer",
	}
	app.handler.users.Create(ctx, ldapUser)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("password", "newpass123")

	req, _ := http.NewRequest("POST", app.server.URL+fmt.Sprintf("/admin/users/%d/password", ldapUser.ID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for non-builtin user password reset, got %d", resp.StatusCode)
	}
}

func TestSelfServiceChangePasswordSuccess(t *testing.T) {
	app := setupTestApp(t)

	ctx := context.Background()
	hash, _ := auth.HashPassword("myoldpass")
	user := &database.User{
		Username: "selfchange", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "selfchange", "myoldpass")
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after login")
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("current_password", "myoldpass")
	form.Set("new_password", "mynewpass")
	form.Set("confirm_password", "mynewpass")

	req, _ := http.NewRequest("POST", app.server.URL+"/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Password changed successfully") {
		t.Error("expected success message in response")
	}

	// Verify login with new password works
	newCookies := loginUser(t, app, "selfchange", "mynewpass")
	if len(newCookies) == 0 {
		t.Error("expected login to succeed with new password")
	}
}

func TestSelfServiceChangePasswordWrongCurrent(t *testing.T) {
	app := setupTestApp(t)

	ctx := context.Background()
	hash, _ := auth.HashPassword("correctpass")
	user := &database.User{
		Username: "wrongcurrent", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "wrongcurrent", "correctpass")
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after login")
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("current_password", "wrongpass")
	form.Set("new_password", "newpass")
	form.Set("confirm_password", "newpass")

	req, _ := http.NewRequest("POST", app.server.URL+"/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Current password is incorrect") {
		t.Error("expected error message about incorrect current password")
	}
}

func TestSelfServiceChangePasswordMismatch(t *testing.T) {
	app := setupTestApp(t)

	ctx := context.Background()
	hash, _ := auth.HashPassword("mypass")
	user := &database.User{
		Username: "mismatch", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "mismatch", "mypass")
	if len(cookies) == 0 {
		t.Fatal("expected session cookie after login")
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("current_password", "mypass")
	form.Set("new_password", "newpass1")
	form.Set("confirm_password", "newpass2")

	req, _ := http.NewRequest("POST", app.server.URL+"/profile/password", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "New passwords do not match") {
		t.Error("expected error message about password mismatch")
	}
}

func TestSearchAPIReturnsResultsAfterIndexing(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "searchable", "Searchable Docs", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("searchable", "v1.0.0")
	versionPath := storage.VersionPath("searchable", "v1.0.0")

	os.WriteFile(filepath.Join(versionPath, "index.html"),
		[]byte("<html><head><title>Getting Started</title></head><body><p>Welcome to the comprehensive installation guide for our software.</p></body></html>"), 0644)
	os.WriteFile(filepath.Join(versionPath, "advanced.html"),
		[]byte("<html><head><title>Advanced Configuration</title></head><body><p>This section covers advanced configuration and tuning parameters.</p></body></html>"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	// Index synchronously for the test
	err := app.handler.searchIndex.IndexVersion(project.ID, version.ID, "searchable", "Searchable Docs", "v1.0.0", versionPath)
	if err != nil {
		t.Fatal("indexing failed:", err)
	}

	// Search for "installation"
	resp, err := http.Get(app.server.URL + "/api/search?q=installation")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "searchable") {
		t.Error("expected project slug in search results")
	}
	if !strings.Contains(bodyStr, "Getting Started") {
		t.Error("expected page title in search results")
	}
}

func TestSearchAPIAccessControlFiltersPrivateProjects(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)

	// Create public and private projects
	pubProject := seedProject(t, app, "public-search", "Public Search", true)
	privProject := seedProject(t, app, "private-search", "Private Search", false)

	ctx := context.Background()
	storage := app.handler.storage

	// Set up public project docs
	storage.EnsureVersionDir("public-search", "v1.0.0")
	pubPath := storage.VersionPath("public-search", "v1.0.0")
	os.WriteFile(filepath.Join(pubPath, "index.html"),
		[]byte("<html><body><p>Public documentation about widgets</p></body></html>"), 0644)

	pubVersion := &database.Version{
		ProjectID: pubProject.ID, Tag: "v1.0.0",
		StoragePath: pubPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, pubVersion)
	app.handler.searchIndex.IndexVersion(pubProject.ID, pubVersion.ID, "public-search", "Public Search", "v1.0.0", pubPath)

	// Set up private project docs
	storage.EnsureVersionDir("private-search", "v1.0.0")
	privPath := storage.VersionPath("private-search", "v1.0.0")
	os.WriteFile(filepath.Join(privPath, "index.html"),
		[]byte("<html><body><p>Private documentation about widgets</p></body></html>"), 0644)

	privVersion := &database.Version{
		ProjectID: privProject.ID, Tag: "v1.0.0",
		StoragePath: privPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, privVersion)
	app.handler.searchIndex.IndexVersion(privProject.ID, privVersion.ID, "private-search", "Private Search", "v1.0.0", privPath)

	// Anonymous search should only see public results
	resp, err := http.Get(app.server.URL + "/api/search?q=widgets&all_versions=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "public-search") {
		t.Error("expected public project in anonymous search results")
	}
	if strings.Contains(bodyStr, "private-search") {
		t.Error("private project should NOT appear in anonymous search results")
	}
}

func TestAdminReindexEndpoint(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/reindex", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after reindex, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc != "/admin/projects?msg=reindex_started" {
		t.Errorf("expected redirect to /admin/projects?msg=reindex_started, got %s", loc)
	}
}

func TestAdminReindexRequiresAdmin(t *testing.T) {
	app := setupTestApp(t)

	// Create a viewer user
	ctx := context.Background()
	hash, _ := auth.HashPassword("viewer123")
	app.handler.users.Create(ctx, &database.User{
		Username: "viewer", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	})

	cookies := loginUser(t, app, "viewer", "viewer123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/reindex", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin reindex, got %d", resp.StatusCode)
	}
}

func TestSearchPageRendered(t *testing.T) {
	app := setupTestApp(t)

	resp, err := http.Get(app.server.URL + "/search")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Search Documentation") {
		t.Error("expected search page content")
	}
}

func TestSearchPageWithQuery(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "page-search", "Page Search", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("page-search", "v1.0.0")
	versionPath := storage.VersionPath("page-search", "v1.0.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"),
		[]byte("<html><head><title>Test Page</title></head><body><p>Unique searchable content about foobar</p></body></html>"), 0644)

	version := &database.Version{
		ProjectID: project.ID, Tag: "v1.0.0",
		StoragePath: versionPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, version)
	app.handler.searchIndex.IndexVersion(project.ID, version.ID, "page-search", "Page Search", "v1.0.0", versionPath)

	resp, err := http.Get(app.server.URL + "/search?q=foobar&all_versions=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "page-search") || !strings.Contains(bodyStr, "Test Page") {
		t.Error("expected search results on search page")
	}
}

func TestSearchPageAccessControlAnonymousSeesPublicOnly(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)

	// Create public and private projects
	pubProject := seedProject(t, app, "pub-page-search", "Public Page Search", true)
	privProject := seedProject(t, app, "priv-page-search", "Private Page Search", false)

	ctx := context.Background()
	storage := app.handler.storage

	// Set up public project docs
	storage.EnsureVersionDir("pub-page-search", "v1.0.0")
	pubPath := storage.VersionPath("pub-page-search", "v1.0.0")
	os.WriteFile(filepath.Join(pubPath, "index.html"),
		[]byte("<html><body><p>Public page about bananas</p></body></html>"), 0644)
	pubVersion := &database.Version{
		ProjectID: pubProject.ID, Tag: "v1.0.0",
		StoragePath: pubPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, pubVersion)
	app.handler.searchIndex.IndexVersion(pubProject.ID, pubVersion.ID, "pub-page-search", "Public Page Search", "v1.0.0", pubPath)

	// Set up private project docs
	storage.EnsureVersionDir("priv-page-search", "v1.0.0")
	privPath := storage.VersionPath("priv-page-search", "v1.0.0")
	os.WriteFile(filepath.Join(privPath, "index.html"),
		[]byte("<html><body><p>Private page about bananas</p></body></html>"), 0644)
	privVersion := &database.Version{
		ProjectID: privProject.ID, Tag: "v1.0.0",
		StoragePath: privPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, privVersion)
	app.handler.searchIndex.IndexVersion(privProject.ID, privVersion.ID, "priv-page-search", "Private Page Search", "v1.0.0", privPath)

	// Anonymous search via page
	resp, err := http.Get(app.server.URL + "/search?q=bananas&all_versions=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "pub-page-search") {
		t.Error("anonymous user should see public project in search page results")
	}
	if strings.Contains(bodyStr, "priv-page-search") {
		t.Error("anonymous user should NOT see private project in search page results")
	}
}

func TestSearchPageAccessControlUserWithAccess(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)

	// Create private project
	privProject := seedProject(t, app, "priv-page-access", "Private Page Access", false)

	ctx := context.Background()
	storage := app.handler.storage

	// Set up private project docs
	storage.EnsureVersionDir("priv-page-access", "v1.0.0")
	privPath := storage.VersionPath("priv-page-access", "v1.0.0")
	os.WriteFile(filepath.Join(privPath, "index.html"),
		[]byte("<html><body><p>Private page about oranges</p></body></html>"), 0644)
	privVersion := &database.Version{
		ProjectID: privProject.ID, Tag: "v1.0.0",
		StoragePath: privPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, privVersion)
	app.handler.searchIndex.IndexVersion(privProject.ID, privVersion.ID, "priv-page-access", "Private Page Access", "v1.0.0", privPath)

	// Create user with access
	hash, _ := auth.HashPassword("user123")
	accessUser := &database.User{
		Username: "pageaccessuser", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, accessUser)
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: privProject.ID,
		UserID:    accessUser.ID,
		Role:      "viewer",
	})

	cookies := loginUser(t, app, "pageaccessuser", "user123")

	req, _ := http.NewRequest("GET", app.server.URL+"/search?q=oranges&all_versions=1", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "priv-page-access") {
		t.Error("user with access should see private project in search page results")
	}
}

func TestSearchPageAccessControlUserWithoutAccess(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)

	// Create private project
	privProject := seedProject(t, app, "priv-page-noaccess", "Private Page No Access", false)

	ctx := context.Background()
	storage := app.handler.storage

	// Set up private project docs
	storage.EnsureVersionDir("priv-page-noaccess", "v1.0.0")
	privPath := storage.VersionPath("priv-page-noaccess", "v1.0.0")
	os.WriteFile(filepath.Join(privPath, "index.html"),
		[]byte("<html><body><p>Private page about apples</p></body></html>"), 0644)
	privVersion := &database.Version{
		ProjectID: privProject.ID, Tag: "v1.0.0",
		StoragePath: privPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, privVersion)
	app.handler.searchIndex.IndexVersion(privProject.ID, privVersion.ID, "priv-page-noaccess", "Private Page No Access", "v1.0.0", privPath)

	// Create user WITHOUT access
	hash, _ := auth.HashPassword("user123")
	noAccessUser := &database.User{
		Username: "pagenoaccessuser", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, noAccessUser)

	cookies := loginUser(t, app, "pagenoaccessuser", "user123")

	req, _ := http.NewRequest("GET", app.server.URL+"/search?q=apples&all_versions=1", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if strings.Contains(bodyStr, "priv-page-noaccess") {
		t.Error("user without access should NOT see private project in search page results")
	}
}

func TestSearchPageAccessControlAdminSeesAll(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)

	// Create private project
	privProject := seedProject(t, app, "priv-page-admin", "Private Page Admin", false)

	ctx := context.Background()
	storage := app.handler.storage

	// Set up private project docs
	storage.EnsureVersionDir("priv-page-admin", "v1.0.0")
	privPath := storage.VersionPath("priv-page-admin", "v1.0.0")
	os.WriteFile(filepath.Join(privPath, "index.html"),
		[]byte("<html><body><p>Private page about mangoes</p></body></html>"), 0644)
	privVersion := &database.Version{
		ProjectID: privProject.ID, Tag: "v1.0.0",
		StoragePath: privPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, privVersion)
	app.handler.searchIndex.IndexVersion(privProject.ID, privVersion.ID, "priv-page-admin", "Private Page Admin", "v1.0.0", privPath)

	cookies := loginUser(t, app, "admin", "admin123")

	req, _ := http.NewRequest("GET", app.server.URL+"/search?q=mangoes&all_versions=1", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "priv-page-admin") {
		t.Error("admin should see private project in search page results")
	}
}

func TestSearchAPIAccessControlUserWithoutAccess(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)

	// Create private project
	privProject := seedProject(t, app, "private-search", "Private Search", false)

	ctx := context.Background()
	storage := app.handler.storage

	// Set up private project docs
	storage.EnsureVersionDir("private-search", "v1.0.0")
	privPath := storage.VersionPath("private-search", "v1.0.0")
	os.WriteFile(filepath.Join(privPath, "index.html"),
		[]byte("<html><body><p>Secret documentation about gadgets</p></body></html>"), 0644)

	privVersion := &database.Version{
		ProjectID: privProject.ID, Tag: "v1.0.0",
		StoragePath: privPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, privVersion)
	app.handler.searchIndex.IndexVersion(privProject.ID, privVersion.ID, "private-search", "Private Search", "v1.0.0", privPath)

	// Create a user WITHOUT access to the private project
	hash, _ := auth.HashPassword("user123")
	noAccessUser := &database.User{
		Username: "noaccess", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, noAccessUser)

	cookies := loginUser(t, app, "noaccess", "user123")

	// Search as logged-in user without access
	req, _ := http.NewRequest("GET", app.server.URL+"/api/search?q=gadgets&all_versions=1", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if strings.Contains(bodyStr, "private-search") {
		t.Error("logged-in user without access should NOT see private project in search results")
	}
}

func TestSearchAPIAccessControlUserWithAccess(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)

	// Create private project
	privProject := seedProject(t, app, "private-access", "Private Access", false)

	ctx := context.Background()
	storage := app.handler.storage

	// Set up private project docs
	storage.EnsureVersionDir("private-access", "v1.0.0")
	privPath := storage.VersionPath("private-access", "v1.0.0")
	os.WriteFile(filepath.Join(privPath, "index.html"),
		[]byte("<html><body><p>Private documentation about gizmos</p></body></html>"), 0644)

	privVersion := &database.Version{
		ProjectID: privProject.ID, Tag: "v1.0.0",
		StoragePath: privPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, privVersion)
	app.handler.searchIndex.IndexVersion(privProject.ID, privVersion.ID, "private-access", "Private Access", "v1.0.0", privPath)

	// Create a user WITH access to the private project
	hash, _ := auth.HashPassword("user123")
	accessUser := &database.User{
		Username: "hasaccess", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, accessUser)

	// Grant access
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: privProject.ID,
		UserID:    accessUser.ID,
		Role:      "viewer",
	})

	cookies := loginUser(t, app, "hasaccess", "user123")

	// Search as logged-in user with access
	req, _ := http.NewRequest("GET", app.server.URL+"/api/search?q=gizmos&all_versions=1", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "private-access") {
		t.Error("logged-in user WITH access should see private project in search results")
	}
}

func TestSearchAPIAccessControlAdminSeesAll(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)

	// Create private project
	privProject := seedProject(t, app, "admin-search-test", "Admin Search Test", false)

	ctx := context.Background()
	storage := app.handler.storage

	// Set up private project docs
	storage.EnsureVersionDir("admin-search-test", "v1.0.0")
	privPath := storage.VersionPath("admin-search-test", "v1.0.0")
	os.WriteFile(filepath.Join(privPath, "index.html"),
		[]byte("<html><body><p>Admin-only documentation about thingamajigs</p></body></html>"), 0644)

	privVersion := &database.Version{
		ProjectID: privProject.ID, Tag: "v1.0.0",
		StoragePath: privPath, UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, privVersion)
	app.handler.searchIndex.IndexVersion(privProject.ID, privVersion.ID, "admin-search-test", "Admin Search Test", "v1.0.0", privPath)

	cookies := loginUser(t, app, "admin", "admin123")

	// Search as admin
	req, _ := http.NewRequest("GET", app.server.URL+"/api/search?q=thingamajigs&all_versions=1", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "admin-search-test") {
		t.Error("admin should see all projects in search results including private ones")
	}
}

func TestProjectTokensPageRequiresEditorAccess(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "token-proj", "Token Project", true)

	ctx := context.Background()

	// Create a viewer user
	hash, _ := auth.HashPassword("viewer123")
	viewer := &database.User{
		Username: "viewer", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, viewer)

	// Grant viewer access (not editor)
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    viewer.ID,
		Role:      "viewer",
	})

	cookies := loginUser(t, app, "viewer", "viewer123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/project/token-proj/tokens", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for viewer accessing tokens page, got %d", resp.StatusCode)
	}
}

func TestProjectTokensPageEditorCanAccess(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "editor-token-proj", "Editor Token Project", true)

	ctx := context.Background()

	// Create an editor user
	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "editor", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, editor)

	// Grant editor access
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    editor.ID,
		Role:      "editor",
	})

	cookies := loginUser(t, app, "editor", "editor123")

	req, _ := http.NewRequest("GET", app.server.URL+"/project/editor-token-proj/tokens", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for editor accessing tokens page, got %d", resp.StatusCode)
	}
}

func TestProjectCreateTokenAlwaysScopedToProject(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "scoped-proj", "Scoped Project", true)

	ctx := context.Background()

	// Create an editor user
	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "tokeneditor", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, editor)

	// Grant editor access
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    editor.ID,
		Role:      "editor",
	})

	cookies := loginUser(t, app, "tokeneditor", "editor123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Create token via project page
	form := url.Values{}
	form.Set("name", "my-ci-token")

	req, _ := http.NewRequest("POST", app.server.URL+"/project/scoped-proj/tokens", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after token creation, got %d", resp.StatusCode)
	}

	// Verify token was created and is scoped to the project
	tokens, err := app.handler.tokens.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token for project, got %d", len(tokens))
	}

	token := tokens[0]
	if token.ProjectID == nil {
		t.Error("token created via project page should be scoped (project_id should not be nil)")
	}
	if token.ProjectID != nil && *token.ProjectID != project.ID {
		t.Errorf("token should be scoped to project %d, got %d", project.ID, *token.ProjectID)
	}
	if token.Name != "my-ci-token" {
		t.Errorf("expected token name 'my-ci-token', got %q", token.Name)
	}
}

func TestProjectRevokeTokenValidatesOwnership(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)

	// Create two projects
	project1 := seedProject(t, app, "proj1", "Project 1", true)
	project2 := seedProject(t, app, "proj2", "Project 2", true)

	ctx := context.Background()

	// Create a token scoped to project1
	token := &database.APIToken{
		UserID:    admin.ID,
		ProjectID: &project1.ID,
		TokenHash: "test-hash",
		Name:      "test-token",
		Scopes:    "upload",
	}
	app.handler.tokens.Create(ctx, token)

	// Create an editor for project2
	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "proj2editor", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, editor)
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project2.ID,
		UserID:    editor.ID,
		Role:      "editor",
	})

	cookies := loginUser(t, app, "proj2editor", "editor123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Try to revoke project1's token from project2's tokens page
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/project/proj2/tokens/%d/revoke", app.server.URL, token.ID), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 when trying to revoke token from wrong project, got %d", resp.StatusCode)
	}

	// Verify token still exists
	_, err = app.handler.tokens.GetByID(ctx, token.ID)
	if err != nil {
		t.Error("token should still exist after failed revoke attempt")
	}
}

func TestProjectRevokeTokenSuccess(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "revoke-proj", "Revoke Project", true)

	ctx := context.Background()

	// Create a token scoped to project
	token := &database.APIToken{
		UserID:    admin.ID,
		ProjectID: &project.ID,
		TokenHash: "revoke-test-hash",
		Name:      "revoke-test-token",
		Scopes:    "upload",
	}
	app.handler.tokens.Create(ctx, token)

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Revoke token
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/project/revoke-proj/tokens/%d/revoke", app.server.URL, token.ID), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after revoke, got %d", resp.StatusCode)
	}

	// Verify token is deleted
	_, err = app.handler.tokens.GetByID(ctx, token.ID)
	if err == nil {
		t.Error("token should be deleted after revoke")
	}
}

func TestAPIUploadWithProjectScopedTokenCorrectProject(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "scoped-upload", "Scoped Upload", true)

	ctx := context.Background()

	// Create robot user
	robot := &database.User{
		Username:   "scoped-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	// Grant upload access
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    robot.ID,
		Role:      "editor",
	})

	// Generate project-scoped token
	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    robot.ID,
		ProjectID: &project.ID,
		TokenHash: tokenHash,
		Name:      "scoped-token",
		Scopes:    "upload",
	})

	// Build multipart upload
	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html>Scoped upload</html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/api/project/scoped-upload/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(respBody))
	}
}

func TestAPIUploadWithProjectScopedTokenWrongProject(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project1 := seedProject(t, app, "scoped-proj1", "Scoped Proj 1", true)
	project2 := seedProject(t, app, "scoped-proj2", "Scoped Proj 2", true)

	ctx := context.Background()

	// Create robot user
	robot := &database.User{
		Username:   "scoped-bot2",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	// Grant upload access to both projects
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project1.ID, UserID: robot.ID, Role: "editor",
	})
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project2.ID, UserID: robot.ID, Role: "editor",
	})

	// Generate token scoped to project1 ONLY
	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID:    robot.ID,
		ProjectID: &project1.ID,
		TokenHash: tokenHash,
		Name:      "proj1-only-token",
		Scopes:    "upload",
	})

	// Try to upload to project2 with project1's token
	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html>Wrong project upload</html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/api/project/scoped-proj2/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for scoped token on wrong project, got %d", resp.StatusCode)
	}
}

func TestAdminCanCreateGlobalToken(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	ctx := context.Background()

	// Create robot user
	robot := &database.User{
		Username:   "global-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Create token without project_id (global)
	form := url.Values{}
	form.Set("name", "global-token")
	// Note: no project_id field = global token

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/robots/%d/tokens", app.server.URL, robot.ID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after token creation, got %d", resp.StatusCode)
	}

	// Verify token was created as global
	tokens, err := app.handler.tokens.ListByUser(ctx, robot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}

	if tokens[0].ProjectID != nil {
		t.Error("admin-created token without project_id should be global (nil)")
	}
}

func TestAdminCanCreateProjectScopedToken(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "admin-scoped", "Admin Scoped", true)

	ctx := context.Background()

	// Create robot user
	robot := &database.User{
		Username:   "admin-scoped-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Create token with project_id
	form := url.Values{}
	form.Set("name", "scoped-token")
	form.Set("project_id", fmt.Sprintf("%d", project.ID))

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/robots/%d/tokens", app.server.URL, robot.ID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after token creation, got %d", resp.StatusCode)
	}

	// Verify token was created as scoped
	tokens, err := app.handler.tokens.ListByUser(ctx, robot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}

	if tokens[0].ProjectID == nil {
		t.Error("admin-created token with project_id should be scoped")
	}
	if tokens[0].ProjectID != nil && *tokens[0].ProjectID != project.ID {
		t.Errorf("expected project_id %d, got %d", project.ID, *tokens[0].ProjectID)
	}
}

// OAuth2 tests - when OAuth2 is not configured, these endpoints should return 501
func TestOAuth2InitiateNotConfigured(t *testing.T) {
	app := setupTestApp(t)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/auth/oauth2", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Without OAuth2 configured, should return 501 Not Implemented
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("expected 501 when OAuth2 not configured, got %d", resp.StatusCode)
	}
}

func TestOAuth2CallbackNotConfigured(t *testing.T) {
	app := setupTestApp(t)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/auth/callback?code=test&state=test", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Without OAuth2 configured, should return 501 Not Implemented
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("expected 501 when OAuth2 not configured, got %d", resp.StatusCode)
	}
}

// Version deletion tests
func TestDeleteVersionSuccess(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "delete-ver", "Delete Version", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("delete-ver", "v1.0.0")
	versionPath := storage.VersionPath("delete-ver", "v1.0.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"), []byte("<html>Delete me</html>"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/project/delete-ver/version/v1.0.0/delete", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after delete, got %d", resp.StatusCode)
	}

	// Verify version is deleted from database
	_, err = app.handler.versions.GetByProjectAndTag(ctx, project.ID, "v1.0.0")
	if err == nil {
		t.Error("version should be deleted from database")
	}
}

func TestDeleteVersionRequiresEditorAccess(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "nodelete-ver", "No Delete Version", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("nodelete-ver", "v1.0.0")
	versionPath := storage.VersionPath("nodelete-ver", "v1.0.0")

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	// Create a viewer user
	hash, _ := auth.HashPassword("viewer123")
	viewer := &database.User{
		Username: "delviewer", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, viewer)

	// Grant viewer access (not editor)
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    viewer.ID,
		Role:      "viewer",
	})

	cookies := loginUser(t, app, "delviewer", "viewer123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/project/nodelete-ver/version/v1.0.0/delete", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for viewer trying to delete version, got %d", resp.StatusCode)
	}
}

func TestDeleteVersionEditorWithAccessCanDelete(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "editor-delete", "Editor Delete", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("editor-delete", "v2.0.0")
	versionPath := storage.VersionPath("editor-delete", "v2.0.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"), []byte("<html>Editor delete</html>"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v2.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	// Create an editor user
	hash, _ := auth.HashPassword("editor123")
	editor := &database.User{
		Username: "deletedit", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, editor)

	// Grant editor access
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    editor.ID,
		Role:      "editor",
	})

	cookies := loginUser(t, app, "deletedit", "editor123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/project/editor-delete/version/v2.0.0/delete", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for editor with access, got %d", resp.StatusCode)
	}

	// Verify version is deleted
	_, err = app.handler.versions.GetByProjectAndTag(ctx, project.ID, "v2.0.0")
	if err == nil {
		t.Error("version should be deleted by editor with access")
	}
}

func TestDeleteVersionNotFound(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	seedProject(t, app, "no-ver", "No Version", true)

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", app.server.URL+"/project/no-ver/version/v99.0.0/delete", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent version, got %d", resp.StatusCode)
	}
}

// Profile page tests
func TestProfilePageRequiresAuth(t *testing.T) {
	app := setupTestApp(t)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/profile", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should redirect to login
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for unauthenticated profile access, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "/login") {
		t.Errorf("expected redirect to /login, got %s", loc)
	}
}

func TestProfilePageRendered(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("user123")
	user := &database.User{
		Username: "profileuser", Password: &hash,
		Email: "profile@example.com",
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "profileuser", "user123")

	req, _ := http.NewRequest("GET", app.server.URL+"/profile", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for authenticated profile access, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "profileuser") {
		t.Error("expected username in profile page")
	}
	if !strings.Contains(bodyStr, "profile@example.com") {
		t.Error("expected email in profile page")
	}
}

// Admin access grant/revoke tests
func TestAdminGrantAccess(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "grant-proj", "Grant Project", false)

	ctx := context.Background()

	// Create a user to grant access to
	hash, _ := auth.HashPassword("user123")
	user := &database.User{
		Username: "grantee", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("grant_user_id", fmt.Sprintf("%d", user.ID))
	form.Set("grant_role", "editor")

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects/grant-proj/access/grant", strings.NewReader(form.Encode()))
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
		t.Errorf("expected 303 redirect after grant, got %d", resp.StatusCode)
	}

	// Verify access was granted
	access, err := app.handler.access.GetAccess(ctx, project.ID, user.ID)
	if err != nil || access == nil {
		t.Error("expected access to be granted")
	}
	if access != nil && access.Role != "editor" {
		t.Errorf("expected role 'editor', got %q", access.Role)
	}
}

func TestAdminGrantAccessRequiresAdmin(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	_ = seedProject(t, app, "noadmin-grant", "No Admin Grant", false)

	ctx := context.Background()

	// Create a non-admin user
	hash, _ := auth.HashPassword("user123")
	user := &database.User{
		Username: "notadmin", Password: &hash,
		AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "notadmin", "user123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("grant_user_id", fmt.Sprintf("%d", user.ID))
	form.Set("grant_role", "editor")

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects/noadmin-grant/access/grant", strings.NewReader(form.Encode()))
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
		t.Errorf("expected 403 for non-admin, got %d", resp.StatusCode)
	}
}

func TestAdminRevokeAccess(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "revoke-proj", "Revoke Project", false)

	ctx := context.Background()

	// Create a user and grant access
	hash, _ := auth.HashPassword("user123")
	user := &database.User{
		Username: "revokee", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, user)
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    user.ID,
		Role:      "editor",
	})

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("user_id", fmt.Sprintf("%d", user.ID))

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects/revoke-proj/access/revoke", strings.NewReader(form.Encode()))
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
		t.Errorf("expected 303 redirect after revoke, got %d", resp.StatusCode)
	}

	// Verify access was revoked
	access, _ := app.handler.access.GetAccess(ctx, project.ID, user.ID)
	if access != nil {
		t.Error("expected access to be revoked")
	}
}

func TestAdminRevokeAccessRequiresAdmin(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	_ = seedProject(t, app, "noadmin-revoke", "No Admin Revoke", false)

	ctx := context.Background()

	// Create a non-admin user
	hash, _ := auth.HashPassword("user123")
	user := &database.User{
		Username: "notadmin2", Password: &hash,
		AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "notadmin2", "user123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("user_id", fmt.Sprintf("%d", user.ID))

	req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects/noadmin-revoke/access/revoke", strings.NewReader(form.Encode()))
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
		t.Errorf("expected 403 for non-admin, got %d", resp.StatusCode)
	}
}

// Admin robot token revoke tests
func TestAdminRevokeRobotToken(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	ctx := context.Background()

	// Create robot user
	robot := &database.User{
		Username:   "revoke-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	// Create token for robot
	token := &database.APIToken{
		UserID:    robot.ID,
		TokenHash: "revoke-hash",
		Name:      "revoke-token",
		Scopes:    "upload",
	}
	app.handler.tokens.Create(ctx, token)

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/robots/%d/tokens/%d/revoke", app.server.URL, robot.ID, token.ID), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after token revoke, got %d", resp.StatusCode)
	}

	// Verify token was deleted
	_, err = app.handler.tokens.GetByID(ctx, token.ID)
	if err == nil {
		t.Error("token should be deleted after revoke")
	}
}

func TestAdminRevokeRobotTokenRequiresAdmin(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	ctx := context.Background()

	// Create robot user
	robot := &database.User{
		Username:   "norevokebot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	// Create token for robot
	token := &database.APIToken{
		UserID:    robot.ID,
		TokenHash: "norevoke-hash",
		Name:      "norevoke-token",
		Scopes:    "upload",
	}
	app.handler.tokens.Create(ctx, token)

	// Create non-admin user
	hash, _ := auth.HashPassword("user123")
	user := &database.User{
		Username: "notadmin3", Password: &hash,
		AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "notadmin3", "user123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/robots/%d/tokens/%d/revoke", app.server.URL, robot.ID, token.ID), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", resp.StatusCode)
	}
}

// Admin delete robot tests
func TestAdminDeleteRobot(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	ctx := context.Background()

	// Create robot user
	robot := &database.User{
		Username:   "delete-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/robots/%d/delete", app.server.URL, robot.ID), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect after robot delete, got %d", resp.StatusCode)
	}

	// Verify robot was deleted
	_, err = app.handler.users.GetByUsername(ctx, "delete-bot")
	if err == nil {
		t.Error("robot should be deleted")
	}
}

func TestAdminDeleteRobotRequiresAdmin(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)

	ctx := context.Background()

	// Create robot user
	robot := &database.User{
		Username:   "nodelete-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	app.handler.users.Create(ctx, robot)

	// Create non-admin user
	hash, _ := auth.HashPassword("user123")
	user := &database.User{
		Username: "notadmin4", Password: &hash,
		AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, user)

	cookies := loginUser(t, app, "notadmin4", "user123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/admin/robots/%d/delete", app.server.URL, robot.ID), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", resp.StatusCode)
	}

	// Verify robot was NOT deleted
	_, err = app.handler.users.GetByUsername(ctx, "nodelete-bot")
	if err != nil {
		t.Error("robot should NOT be deleted by non-admin")
	}
}

// Ensure the interface is satisfied
var _ fs.FS = (fs.FS)(nil)

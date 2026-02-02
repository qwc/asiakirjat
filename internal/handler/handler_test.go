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
	project := &database.Project{
		Slug:     slug,
		Name:     name,
		IsPublic: isPublic,
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

func TestUploadDuplicateVersion(t *testing.T) {
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

	zipBuf := createTestZip(t, map[string]string{"index.html": "new"})

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

	// Should re-render upload page with error (200), not redirect
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (upload page with error), got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), "tag may already exist") {
		t.Error("expected duplicate version error message")
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
	form.Set("is_public", "1")

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
	if !project.IsPublic {
		t.Error("expected project to be public")
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
	form.Set("is_public", "1")

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
	if !project.IsPublic {
		t.Error("expected project to be public after update")
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

// Ensure the interface is satisfied
var _ fs.FS = (fs.FS)(nil)

package handler

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
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

// Ensure the interface is satisfied
var _ fs.FS = (fs.FS)(nil)

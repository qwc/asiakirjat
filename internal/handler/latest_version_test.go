package handler

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
)

// noRedirectClient returns an http.Client that does not follow redirects, so
// tests can inspect 3xx Location headers directly.
func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// seedVersionWithIndex creates a version both in the DB and on disk, writing an
// index.html whose body is the given marker so tests can tell versions apart.
func seedVersionWithIndex(t *testing.T, app *testApp, project *database.Project, tag, marker string) {
	t.Helper()
	ctx := context.Background()
	storage := app.handler.storage
	if err := storage.EnsureVersionDir(project.Slug, tag); err != nil {
		t.Fatal(err)
	}
	versionPath := storage.VersionPath(project.Slug, tag)
	if err := os.WriteFile(filepath.Join(versionPath, "index.html"), []byte("<html>"+marker+"</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.versions.Create(ctx, &database.Version{
		ProjectID: project.ID, Tag: tag, StoragePath: versionPath, ContentType: "archive",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLatestServesNewestInPlace(t *testing.T) {
	app := setupTestApp(t)
	project := seedProject(t, app, "docs", "Documentation", true) // public
	seedVersionWithIndex(t, app, project, "v1.0.0", "OLD CONTENT")
	seedVersionWithIndex(t, app, project, "v2.0.0", "NEW CONTENT")

	resp, err := http.Get(app.server.URL + "/project/docs/latest/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 served in place, got %d", resp.StatusCode)
	}
	// Served at /latest/ — not redirected to a versioned URL.
	if resp.Request.URL.Path != "/project/docs/latest/" {
		t.Errorf("expected URL to stay at /latest/, got %q", resp.Request.URL.Path)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "NEW CONTENT") {
		t.Errorf("expected newest version content, got: %s", body)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %q", cc)
	}
	// The injected overlay must carry the resolved concrete tag (not the
	// literal "latest") in data-current. The version-switch and compare JS
	// read data-current to know which concrete version is on screen; if it
	// said "latest", the comparison/switch URLs would be malformed.
	if !strings.Contains(string(body), `data-current="v2.0.0"`) {
		t.Errorf("expected overlay data-current to be resolved tag v2.0.0")
	}
}

func TestLatestRespectsPin(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()
	project := seedProject(t, app, "docs", "Documentation", true)
	seedVersionWithIndex(t, app, project, "v1.0.0", "OLD CONTENT")
	seedVersionWithIndex(t, app, project, "v2.0.0", "NEW CONTENT")

	// Pin the older version; latest must follow the pin, not the semver max.
	pinned := "v1.0.0"
	project.PinnedVersion = &pinned
	project.PinPermanent = true
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(app.server.URL + "/project/docs/latest/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "OLD CONTENT") {
		t.Errorf("expected pinned version content, got: %s", body)
	}
}

func TestLatestBareRedirectsToSlash(t *testing.T) {
	app := setupTestApp(t)
	project := seedProject(t, app, "docs", "Documentation", true)
	seedVersionWithIndex(t, app, project, "v1.0.0", "CONTENT")

	resp, err := noRedirectClient().Get(app.server.URL + "/project/docs/latest")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected 301 to add trailing slash, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/project/docs/latest/" {
		t.Errorf("expected redirect to /project/docs/latest/, got %q", got)
	}
}

func TestLatestNoVersions(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "empty", "Empty", true)

	resp, err := noRedirectClient().Get(app.server.URL + "/project/empty/latest/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for project with no versions, got %d", resp.StatusCode)
	}
}

func TestLatestAnonymousOnPrivateRedirectsLogin(t *testing.T) {
	app := setupTestApp(t)
	project := seedProject(t, app, "secret", "Secret", false) // custom/non-public
	seedVersionWithIndex(t, app, project, "v1.0.0", "SECRET")

	resp, err := noRedirectClient().Get(app.server.URL + "/project/secret/latest/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Anonymous user on a non-public project is sent to login, never told the
	// version exists.
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect to login, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/login" {
		t.Errorf("expected redirect to /login, got %q", got)
	}
}

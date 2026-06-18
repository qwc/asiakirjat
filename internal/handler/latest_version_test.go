package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
)

// noRedirectClient returns an http.Client that does not follow redirects, so
// tests can inspect the 3xx Location header directly.
func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func seedTwoVersions(t *testing.T, app *testApp, project *database.Project) {
	t.Helper()
	ctx := context.Background()
	for _, tag := range []string{"v1.0.0", "v2.0.0"} {
		if err := app.handler.versions.Create(ctx, &database.Version{
			ProjectID: project.ID, Tag: tag, StoragePath: "/tmp/test", ContentType: "archive",
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLatestPermalinkRedirectsToNewest(t *testing.T) {
	app := setupTestApp(t)
	project := seedProject(t, app, "docs", "Documentation", true) // public
	seedTwoVersions(t, app, project)

	resp, err := noRedirectClient().Get(app.server.URL + "/project/docs/latest/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/project/docs/v2.0.0/" {
		t.Errorf("expected redirect to /project/docs/v2.0.0/, got %q", got)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("expected Cache-Control no-store, got %q", cc)
	}
}

func TestLatestPermalinkPreservesPath(t *testing.T) {
	app := setupTestApp(t)
	project := seedProject(t, app, "docs", "Documentation", true)
	seedTwoVersions(t, app, project)

	resp, err := noRedirectClient().Get(app.server.URL + "/project/docs/latest/guide/index.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Location"); got != "/project/docs/v2.0.0/guide/index.html" {
		t.Errorf("expected path preserved in redirect, got %q", got)
	}
}

func TestLatestPermalinkRespectsPin(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()
	project := seedProject(t, app, "docs", "Documentation", true)
	seedTwoVersions(t, app, project)

	// Pin the older version; latest must follow the pin, not the semver max.
	pinned := "v1.0.0"
	project.PinnedVersion = &pinned
	project.PinPermanent = true
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	resp, err := noRedirectClient().Get(app.server.URL + "/project/docs/latest/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Location"); got != "/project/docs/v1.0.0/" {
		t.Errorf("expected redirect to pinned v1.0.0, got %q", got)
	}
}

func TestLatestPermalinkNoVersions(t *testing.T) {
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

func TestLatestPermalinkAnonymousOnPrivateRedirectsLogin(t *testing.T) {
	app := setupTestApp(t)
	project := seedProject(t, app, "secret", "Secret", false) // custom/non-public
	seedTwoVersions(t, app, project)

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

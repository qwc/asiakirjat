package handler

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

// releaseNumbers is the shipped default keep pattern: a release number with an
// optional v prefix, and nothing else.
var releaseNumbers = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`).MatchString

// A prerelease outsorts every release below it — v2.0.0-rc1 beats v1.0.0 — so
// it used to become "latest" everywhere and stay there until retention deleted
// it (issue #157). The keep pattern is the project's own definition of a real
// release, so it decides.
func TestLatestVersionTagPrefersAKeptRelease(t *testing.T) {
	versions := []database.Version{{Tag: "v1.0.0"}, {Tag: "v2.0.0-rc1"}}

	// Without a pattern, the sort alone still picks the prerelease. This is
	// the behaviour being corrected, asserted so the case cannot go stale.
	if got := latestVersionTag(versions, nil, nil); got != "v2.0.0-rc1" {
		t.Errorf("expected the semver sort to put the prerelease first, got %s", got)
	}

	if got := latestVersionTag(versions, nil, releaseNumbers); got != "v1.0.0" {
		t.Errorf("expected the newest kept release, got %s", got)
	}

	// A pin is an explicit statement about where readers land; it outranks
	// the pattern, exactly as it outranks the sort.
	pinned := "v2.0.0-rc1"
	if got := latestVersionTag(versions, &pinned, releaseNumbers); got != "v2.0.0-rc1" {
		t.Errorf("expected the pin to win over the keep pattern, got %s", got)
	}

	// A pattern that describes none of a project's tags must not leave it with
	// no latest at all.
	nothing := func(string) bool { return false }
	if got := latestVersionTag(versions, nil, nothing); got != "v2.0.0-rc1" {
		t.Errorf("expected a fallback to the sorted newest, got %s", got)
	}
}

// setKeepPattern gives a project its own definition of a release.
func setKeepPattern(t *testing.T, app *testApp, project *database.Project, pattern string) {
	t.Helper()
	project.VersionKeepPattern = &pattern
	if err := app.handler.projects.Update(context.Background(), project); err != nil {
		t.Fatal(err)
	}
}

// The permalink is the URL people share, so it is the one that must not point
// at a release candidate.
func TestLatestPermalinkSkipsAPrerelease(t *testing.T) {
	app := setupTestApp(t)
	project := seedProject(t, app, "docs", "Documentation", true)
	setKeepPattern(t, app, project, `^v?\d+\.\d+\.\d+$`)
	seedVersionWithIndex(t, app, project, "v1.0.0", "RELEASE")
	seedVersionWithIndex(t, app, project, "v2.0.0-rc1", "CANDIDATE")

	resp, err := http.Get(app.server.URL + "/project/docs/latest/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if !strings.Contains(string(body), "RELEASE") {
		t.Errorf("expected /latest/ to serve the newest kept release; served the candidate: %t",
			strings.Contains(string(body), "CANDIDATE"))
	}
}

// And the project page has to agree with the permalink it prints above the
// version list.
func TestProjectPageLatestAgreesWithTheKeepPattern(t *testing.T) {
	app := setupTestApp(t)
	project := seedProject(t, app, "agrees", "Agrees", true)
	setKeepPattern(t, app, project, `^v?\d+\.\d+\.\d+$`)
	seedVersionWithIndex(t, app, project, "v1.0.0", "RELEASE")
	seedVersionWithIndex(t, app, project, "v2.0.0-rc1", "CANDIDATE")

	body := projectPageBody(t, app, "agrees")

	if !strings.Contains(body, "(currently v1.0.0)") {
		t.Error("expected the permalink hint to name the newest kept release")
	}
	if got := strings.Count(body, "version-badge-latest"); got != 1 {
		t.Errorf("expected exactly one Latest badge, got %d", got)
	}
}

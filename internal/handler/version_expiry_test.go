package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
)

// projectPageBody fetches a public project's detail page as an anonymous
// visitor and returns the rendered HTML.
func projectPageBody(t *testing.T, app *testApp, slug string) string {
	t.Helper()
	resp, err := http.Get(app.server.URL + "/project/" + slug)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /project/%s, got %d", slug, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// setRetention applies a per-project retention window, since the shipped
// default (retention.nonsemver_days = 0) deletes nothing.
func setRetention(t *testing.T, app *testApp, project *database.Project, days int) {
	t.Helper()
	project.RetentionDays = &days
	if err := app.handler.projects.Update(context.Background(), project); err != nil {
		t.Fatal(err)
	}
}

// TestProjectPageMarksExpiringVersions is what issue #149 asks for: a version
// the keep pattern does not match is visibly bound for autodelete, with the
// days it has left.
func TestProjectPageMarksExpiringVersions(t *testing.T) {
	app := setupTestApp(t)

	project := seedProject(t, app, "expiring", "Expiring", true)
	setRetention(t, app, project, 30)

	seedAgedVersion(t, app, project, "v1.0.0", 10)   // release number: kept
	seedAgedVersion(t, app, project, "nightly", 400) // past the window already
	seedAgedVersion(t, app, project, "branch-x", 10) // 20 days left

	body := projectPageBody(t, app, "expiring")

	if !strings.Contains(body, "Expires in 20 days") {
		t.Error("expected branch-x to show the days it has left")
	}
	if !strings.Contains(body, "Expires today") {
		t.Error("expected an already-expired version to be marked as going on the next pass")
	}
	// The kept release must carry no expiry badge. Counting is the only way to
	// tell, since all three versions render into one list.
	if got := strings.Count(body, "version-badge-expiring"); got != 2 {
		t.Errorf("expected exactly the 2 expiring versions to be badged, got %d badges", got)
	}
}

// The hint above the list explains why the badges are there.
func TestProjectPageExplainsRetentionRule(t *testing.T) {
	app := setupTestApp(t)

	project := seedProject(t, app, "explained", "Explained", true)
	pattern := `^release-`
	project.VersionKeepPattern = &pattern
	setRetention(t, app, project, 45)
	seedAgedVersion(t, app, project, "scratch", 1)

	body := projectPageBody(t, app, "explained")

	if !strings.Contains(body, "^release-") {
		t.Error("expected the effective keep pattern to be shown")
	}
	if !strings.Contains(body, "45 days after upload") {
		t.Error("expected the retention window to be shown")
	}
}

// Retention off means nothing is ever deleted, whatever the pattern says — so
// promising an expiry would be a lie. This is the shipped default.
func TestProjectPageOmitsExpiryWhenRetentionDisabled(t *testing.T) {
	app := setupTestApp(t)

	project := seedProject(t, app, "forever", "Forever", true)
	seedAgedVersion(t, app, project, "nightly", 400)

	body := projectPageBody(t, app, "forever")

	if strings.Contains(body, "Expires") {
		t.Error("expected no expiry hint when retention is disabled")
	}
	if strings.Contains(body, "after upload") {
		t.Error("expected no retention notice when retention is disabled")
	}
}

// The badge must agree with enforceRetentionPolicy on pins (issue #141): a
// permanent pin is exempt, a temporary one is not.
func TestProjectPageRespectsPinExemption(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := seedProject(t, app, "pinned", "Pinned", true)
	setRetention(t, app, project, 30)
	seedAgedVersion(t, app, project, "nightly", 400)

	pinned := "nightly"
	project.PinnedVersion = &pinned
	project.PinPermanent = true
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	if body := projectPageBody(t, app, "pinned"); strings.Contains(body, "version-badge-expiring") {
		t.Error("a permanently pinned version is exempt from retention; it must not be badged")
	}

	project.PinPermanent = false
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	if body := projectPageBody(t, app, "pinned"); !strings.Contains(body, "version-badge-expiring") {
		t.Error("a temporary pin does not protect a version from retention; it must still be badged")
	}
}

// A version the pattern keeps says so, instead of leaving the reader to infer
// it from the absence of a badge (issue #157). The claim is only made where it
// means something: with retention off nothing expires, so saying "no
// expiration" on every row of every project would be noise, not information.
func TestProjectPageMarksVersionsThatNeverExpire(t *testing.T) {
	app := setupTestApp(t)

	project := seedProject(t, app, "kept", "Kept", true)
	setRetention(t, app, project, 30)
	seedAgedVersion(t, app, project, "v1.0.0", 10) // release number: kept
	seedAgedVersion(t, app, project, "branch-x", 10)

	body := projectPageBody(t, app, "kept")

	if !strings.Contains(body, "No expiration") {
		t.Error("expected the kept release to be marked as never expiring")
	}
	// One badge for the kept release, and none for the expiring one — the two
	// render into the same list, so counting is the only way to tell.
	if got := strings.Count(body, "version-badge-forever"); got != 1 {
		t.Errorf("expected exactly the 1 kept version to be badged, got %d badges", got)
	}
}

// The counterpart: retention off means every version is permanent, and the
// badge would say nothing a reader does not already know.
func TestProjectPageOmitsNeverExpiresWhenRetentionDisabled(t *testing.T) {
	app := setupTestApp(t)

	project := seedProject(t, app, "unbounded", "Unbounded", true)
	seedAgedVersion(t, app, project, "v1.0.0", 10)

	if body := projectPageBody(t, app, "unbounded"); strings.Contains(body, "No expiration") {
		t.Error("expected no expiry claim at all when retention is disabled")
	}
}

// A permanent pin is exempt from retention (issue #141), so it is one of the
// versions that never expires — the badge has to agree with that, or it
// contradicts the rule the retention pass actually applies.
func TestProjectPagePermanentPinNeverExpires(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := seedProject(t, app, "pinned-forever", "Pinned Forever", true)
	setRetention(t, app, project, 30)
	seedAgedVersion(t, app, project, "nightly", 400)

	pinned := "nightly"
	project.PinnedVersion = &pinned
	project.PinPermanent = true
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	if body := projectPageBody(t, app, "pinned-forever"); !strings.Contains(body, "No expiration") {
		t.Error("a permanently pinned version is exempt from retention; it must say so")
	}
}

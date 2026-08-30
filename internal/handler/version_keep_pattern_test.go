package handler

import (
	"context"

	"github.com/jmoiron/sqlx"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/qwc/asiakirjat/internal/database"
)

// seedAgedVersion records a version as if it had been uploaded daysAgo days
// ago, so retention has something expired to act on.
func seedAgedVersion(t *testing.T, app *testApp, project *database.Project, tag string, daysAgo int) {
	t.Helper()
	ctx := context.Background()

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         tag,
		StoragePath: project.Slug + "/" + tag,
		ContentType: "archive",
	}
	if err := app.handler.versions.Create(ctx, version); err != nil {
		t.Fatal(err)
	}
	// created_at defaults to now and the store has no way to backdate it, so
	// the test writes it directly.
	db := app.db.(*sqlx.DB)
	if _, err := db.Exec(db.Rebind(`UPDATE versions SET created_at = ? WHERE id = ?`),
		time.Now().AddDate(0, 0, -daysAgo), version.ID); err != nil {
		t.Fatal(err)
	}
}

func remainingTags(t *testing.T, app *testApp, projectID int64) []string {
	t.Helper()
	versions, err := app.handler.versions.ListByProject(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	tags := make([]string, 0, len(versions))
	for _, v := range versions {
		tags = append(tags, v.Tag)
	}
	return tags
}

// TestKeepPatternDecidesWhatExpires is the rule issue #127 asks for: the
// project names the versions worth keeping, and everything else is subject to
// the retention period — including semver tags, which the old rule always kept.
func TestKeepPatternDecidesWhatExpires(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := seedProject(t, app, "patterned", "Patterned", true)
	pattern := `^v\d+\.\d+\.\d+$`
	project.VersionKeepPattern = &pattern
	days := 30
	project.RetentionDays = &days
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	seedAgedVersion(t, app, project, "v1.0.0", 400)        // matches: kept
	seedAgedVersion(t, app, project, "v1.1.0-rc1", 400)    // semver-ish but no match: expires
	seedAgedVersion(t, app, project, "feature-login", 400) // no match: expires
	seedAgedVersion(t, app, project, "feature-recent", 1)  // no match but young: kept

	app.handler.enforceRetentionPolicy(ctx, project)

	got := remainingTags(t, app, project.ID)
	want := map[string]bool{"v1.0.0": true, "feature-recent": true}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for _, tag := range got {
		if !want[tag] {
			t.Errorf("did not expect %q to survive retention", tag)
		}
	}
}

// TestInstanceDefaultKeepsReleaseNumbers pins what a project with no pattern of
// its own inherits: retention.keep_pattern, which keeps release numbers and
// expires everything else — including two-component tags and release
// candidates, which the older "looks like semver" rule kept.
func TestInstanceDefaultKeepsReleaseNumbers(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := seedProject(t, app, "default-rule", "Default Rule", true)
	days := 30
	project.RetentionDays = &days
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	seedAgedVersion(t, app, project, "v2.0.0", 400)     // release number: kept
	seedAgedVersion(t, app, project, "3.1.4", 400)      // release number, no v: kept
	seedAgedVersion(t, app, project, "v1.9", 400)       // two components: expires
	seedAgedVersion(t, app, project, "v2.1.0-rc1", 400) // release candidate: expires
	seedAgedVersion(t, app, project, "nightly", 400)    // branch build: expires

	app.handler.enforceRetentionPolicy(ctx, project)

	got := remainingTags(t, app, project.ID)
	want := map[string]bool{"v2.0.0": true, "3.1.4": true}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for _, tag := range got {
		if !want[tag] {
			t.Errorf("did not expect %q to survive the default pattern", tag)
		}
	}
}

// TestInstanceDefaultCanBeWidened: an operator who tags v1.2 changes one
// config value rather than editing every project.
func TestInstanceDefaultCanBeWidened(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	app.handler.config.Retention.KeepPattern = `^v?\d+(\.\d+)*$`

	project := seedProject(t, app, "widened", "Widened", true)
	days := 30
	project.RetentionDays = &days
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	seedAgedVersion(t, app, project, "v1.9", 400)
	seedAgedVersion(t, app, project, "nightly", 400)

	app.handler.enforceRetentionPolicy(ctx, project)

	got := remainingTags(t, app, project.ID)
	if len(got) != 1 || got[0] != "v1.9" {
		t.Errorf("expected the widened default to keep v1.9, got %v", got)
	}
}

// TestProjectPatternOverridesInstanceDefault — the per-project field wins.
func TestProjectPatternOverridesInstanceDefault(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := seedProject(t, app, "override", "Override", true)
	pattern := `^nightly`
	project.VersionKeepPattern = &pattern
	days := 30
	project.RetentionDays = &days
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	seedAgedVersion(t, app, project, "nightly-42", 400) // matches the project rule
	seedAgedVersion(t, app, project, "v2.0.0", 400)     // release, but not what this project keeps

	app.handler.enforceRetentionPolicy(ctx, project)

	got := remainingTags(t, app, project.ID)
	if len(got) != 1 || got[0] != "nightly-42" {
		t.Errorf("expected the project pattern to win over the instance default, got %v", got)
	}
}

// TestInvalidInstancePatternFallsBackToSemver: a broken config value must not
// turn retention into a purge.
func TestInvalidInstancePatternFallsBackToSemver(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	app.handler.config.Retention.KeepPattern = "^v(unclosed"

	project := seedProject(t, app, "broken-config", "Broken Config", true)
	days := 30
	project.RetentionDays = &days
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	seedAgedVersion(t, app, project, "v3.0.0", 400)
	seedAgedVersion(t, app, project, "scratch", 400)

	app.handler.enforceRetentionPolicy(ctx, project)

	got := remainingTags(t, app, project.ID)
	if len(got) != 1 || got[0] != "v3.0.0" {
		t.Errorf("expected the fallback to keep version-shaped tags, got %v", got)
	}
}

// TestInvalidKeepPatternFallsBackToSemver: retention deletes what it does not
// keep, so a pattern that cannot compile must keep more, never wipe history.
func TestInvalidKeepPatternFallsBackToSemver(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := seedProject(t, app, "broken-pattern", "Broken", true)
	broken := "^v(unclosed"
	project.VersionKeepPattern = &broken
	days := 30
	project.RetentionDays = &days
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	seedAgedVersion(t, app, project, "v3.0.0", 400)
	seedAgedVersion(t, app, project, "scratch", 400)

	app.handler.enforceRetentionPolicy(ctx, project)

	got := remainingTags(t, app, project.ID)
	if len(got) != 1 || got[0] != "v3.0.0" {
		t.Errorf("expected the semver fallback to keep v3.0.0, got %v", got)
	}
}

// TestEditFormSavesAndValidatesKeepPattern covers the admin path: a valid
// pattern is stored, an uncompilable one is refused, and clearing the field
// restores the default.
func TestEditFormSavesAndValidatesKeepPattern(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	seedProject(t, app, "editable", "Editable", true)

	form := url.Values{}
	form.Set("slug", "editable")
	form.Set("name", "Editable")
	form.Set("visibility", "public")
	form.Set("version_keep_pattern", `^release-\d+$`)
	if resp := adminPost(t, app, cookies, "/admin/projects/editable/edit", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 saving a valid pattern, got %d", resp.StatusCode)
	}
	project, err := app.handler.projects.GetBySlug(ctx, "editable")
	if err != nil {
		t.Fatal(err)
	}
	if project.VersionKeepPattern == nil || *project.VersionKeepPattern != `^release-\d+$` {
		t.Fatalf("expected the pattern stored, got %v", project.VersionKeepPattern)
	}

	form.Set("version_keep_pattern", "^v(unclosed")
	if resp := adminPost(t, app, cookies, "/admin/projects/editable/edit", form); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for a pattern that cannot compile, got %d", resp.StatusCode)
	}
	project, _ = app.handler.projects.GetBySlug(ctx, "editable")
	if project.VersionKeepPattern == nil || *project.VersionKeepPattern != `^release-\d+$` {
		t.Error("a rejected pattern must not overwrite the stored one")
	}

	form.Set("version_keep_pattern", "")
	if resp := adminPost(t, app, cookies, "/admin/projects/editable/edit", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 clearing the pattern, got %d", resp.StatusCode)
	}
	project, _ = app.handler.projects.GetBySlug(ctx, "editable")
	if project.VersionKeepPattern != nil {
		t.Errorf("expected clearing the field to restore the default, got %q", *project.VersionKeepPattern)
	}
}

// TestPermanentPinSurvivesRetention covers issue #141: retention decided what
// to delete purely from the keep rule and the version's age, so a pinned
// version could be deleted out from under its pin — leaving the project
// pointing at a version that no longer existed and quietly serving a
// different one.
func TestPermanentPinSurvivesRetention(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := seedProject(t, app, "pinned", "Pinned", true)
	days := 30
	project.RetentionDays = &days
	pinned := "nightly-2026-01-01"
	project.PinnedVersion = &pinned
	project.PinPermanent = true
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	seedAgedVersion(t, app, project, pinned, 400)
	seedAgedVersion(t, app, project, "nightly-2026-01-02", 400)

	app.handler.enforceRetentionPolicy(ctx, project)

	got := remainingTags(t, app, project.ID)
	if len(got) != 1 || got[0] != pinned {
		t.Errorf("expected the permanently pinned version to survive and the other to expire, got %v", got)
	}
}

// TestTemporaryPinDoesNotSurviveRetention: a temporary pin is cleared by the
// next upload, so it does not claim the version is worth keeping.
func TestTemporaryPinDoesNotSurviveRetention(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := seedProject(t, app, "temp-pinned", "Temp Pinned", true)
	days := 30
	project.RetentionDays = &days
	pinned := "nightly-2026-01-01"
	project.PinnedVersion = &pinned
	project.PinPermanent = false
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	seedAgedVersion(t, app, project, pinned, 400)

	app.handler.enforceRetentionPolicy(ctx, project)

	if got := remainingTags(t, app, project.ID); len(got) != 0 {
		t.Errorf("expected a temporarily pinned version to expire like any other, got %v", got)
	}
}

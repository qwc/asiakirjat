package handler

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
)

// seedOrg makes an organization and returns it.
func seedOrg(t *testing.T, app *testApp, slug, name string) *database.Org {
	t.Helper()
	org := &database.Org{Slug: slug, Name: name}
	if err := app.handler.orgs.Create(context.Background(), org); err != nil {
		t.Fatal(err)
	}
	return org
}

// seedProjectInOrg makes a public project belonging to a given org.
func seedProjectInOrg(t *testing.T, app *testApp, slug, name string, orgID int64) *database.Project {
	t.Helper()
	p := &database.Project{
		Slug: slug, Name: name,
		Visibility: database.VisibilityPublic,
		Exposure:   database.ExposurePublic,
		OrgID:      &orgID,
	}
	if err := app.handler.projects.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func frontpageBody(t *testing.T, app *testApp) string {
	t.Helper()
	resp, err := http.Get(app.server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// A project's organization must be visible on the frontpage — without it there
// is no way to tell which one a project belongs to.
func TestFrontpageShowsOrganizations(t *testing.T) {
	app := setupTestApp(t)

	platform := seedOrg(t, app, "platform", "Platform Team")
	seedProject(t, app, "ungrouped", "Ungrouped", true)
	seedProjectInOrg(t, app, "runtime", "Runtime", platform.ID)

	body := frontpageBody(t, app)

	if !strings.Contains(body, "Platform Team") {
		t.Error("expected the organization to be named on the frontpage")
	}
	if !strings.Contains(body, `data-org="platform team"`) {
		t.Error("expected an org section the filter can act on")
	}
}

// The default org sorts first: on an installation that has not started using
// organizations, that is where everything is, and burying it under an
// alphabetically earlier org would be strange.
func TestFrontpageGroupsDefaultOrgFirst(t *testing.T) {
	app := setupTestApp(t)

	seedOrg(t, app, "alpha", "Alpha Team")
	seedProject(t, app, "ungrouped", "Ungrouped", true)
	alpha, err := app.handler.orgs.GetBySlug(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	seedProjectInOrg(t, app, "alpha-docs", "Alpha Docs", alpha.ID)

	body := frontpageBody(t, app)
	defaultAt := strings.Index(body, "No Org")
	alphaAt := strings.Index(body, "Alpha Team")
	if defaultAt < 0 || alphaAt < 0 {
		t.Fatalf("expected both organizations on the page (default=%d alpha=%d)", defaultAt, alphaAt)
	}
	if defaultAt > alphaAt {
		t.Error("expected the default organization to come first")
	}
}

// With a single organization, headings would be noise on every page load.
func TestFrontpageOmitsHeadingsForOneOrg(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "only", "Only", true)

	body := frontpageBody(t, app)
	if strings.Contains(body, "org-heading") {
		t.Error("expected no organization headings when there is only one")
	}
	if strings.Contains(body, `id="org-filter"`) {
		t.Error("expected no organization filter when there is only one")
	}
}

// The box on this page filters what is already listed; the full-text search of
// documentation content is elsewhere. Two boxes doing different things need
// different words.
func TestFrontpageFilterIsNotCalledSearch(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "only", "Only", true)

	body := frontpageBody(t, app)
	if !strings.Contains(body, `placeholder="Filter projects..."`) {
		t.Error("expected the project box to be labelled as a filter")
	}
	if strings.Contains(body, `placeholder="Search projects..."`) {
		t.Error("the project filter must not call itself a search")
	}
}

// A project can be put in an organization when it is created, rather than
// created in the wrong one and moved.
func TestCreateProjectIntoAnOrganization(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	platform := seedOrg(t, app, "platform", "Platform Team")

	form := url.Values{}
	form.Set("slug", "runtime")
	form.Set("name", "Runtime")
	form.Set("exposure", "granted")
	form.Set("org_id", strconv.FormatInt(platform.ID, 10))
	if resp := adminPost(t, app, cookies, "/admin/projects", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("creating: expected 303, got %d", resp.StatusCode)
	}

	created, err := app.handler.projects.GetBySlug(ctx, "runtime")
	if err != nil {
		t.Fatal(err)
	}
	if created.OrgID == nil || *created.OrgID != platform.ID {
		t.Errorf("expected the project to land in the chosen org, got %v", created.OrgID)
	}
	if created.Exposure != database.ExposureGranted {
		t.Errorf("expected exposure granted, got %q", created.Exposure)
	}
}

// The create form must not offer the retired visibility values: they all mean
// the same thing now, so choosing between them is a dead end.
func TestCreateFormOffersExposureNotVisibility(t *testing.T) {
	app := setupTestApp(t)

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	status, body := adminGet(t, app, cookies, "/admin/projects")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if !strings.Contains(body, `name="exposure"`) || !strings.Contains(body, `name="org_id"`) {
		t.Error("expected the create form to offer exposure and an organization")
	}
	if strings.Contains(body, `<option value="custom"`) || strings.Contains(body, `<option value="private"`) {
		t.Error("the create form must not offer the retired visibility values")
	}
}

// An anonymous visitor sees public projects. This used to be answered by
// selecting on the visibility column, which the access model retired.
func TestFrontpageAnonymousSeesPublicByExposure(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	open := seedProject(t, app, "open", "Open Docs", true)
	seedProject(t, app, "closed", "Closed Docs", false)

	// Exposure alone decides it: clear the legacy column on the public one and
	// it must still be listed.
	open.Visibility = database.VisibilityCustom
	if err := app.handler.projects.Update(ctx, open); err != nil {
		t.Fatal(err)
	}

	body := frontpageBody(t, app)
	if !strings.Contains(body, "Open Docs") {
		t.Error("expected a public-exposure project to be listed for anonymous visitors")
	}
	if strings.Contains(body, "Closed Docs") {
		t.Error("expected a granted-only project to stay hidden from anonymous visitors")
	}
}

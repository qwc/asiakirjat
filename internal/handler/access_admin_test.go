package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
)

// The admin surface for the unified model (#150, #151). The point of these is
// that what the pages write is what the resolver reads — the failure mode this
// redesign exists to end is a form that saves somewhere nothing consults.

func TestGrantingAGroupOnAProjectLetsItsMembersIn(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	reader := seedViewer(t, app, "reader")
	project := seedProject(t, app, "docs", "Docs", false)

	// A group naming the user, created through the admin pages.
	form := url.Values{}
	form.Set("name", "engineering")
	form.Set("description", "Dev team")
	if resp := adminPost(t, app, cookies, "/admin/access-groups", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("creating the group: expected 303, got %d", resp.StatusCode)
	}
	group, err := app.handler.accessGroups.GetByName(ctx, "engineering")
	if err != nil {
		t.Fatal(err)
	}

	member := url.Values{}
	member.Set("subject_type", "user")
	member.Set("subject_identifier", "reader")
	if resp := adminPost(t, app, cookies, "/admin/access-groups/"+strconv.FormatInt(group.ID, 10)+"/members", member); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("adding the member: expected 303, got %d", resp.StatusCode)
	}

	// Before the grant, the group means nothing for this project.
	if app.handler.resolver.CanView(ctx, reader, project) {
		t.Fatal("a group with no grant must not admit anyone")
	}

	grant := url.Values{}
	grant.Set("subject_kind", "group")
	grant.Set("subject", "engineering")
	grant.Set("role", "editor")
	if resp := adminPost(t, app, cookies, "/admin/projects/docs/grants", grant); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("granting: expected 303, got %d", resp.StatusCode)
	}

	if !app.handler.resolver.CanView(ctx, reader, project) {
		t.Error("expected the granted group's member to be able to view the project")
	}
	if !app.handler.resolver.CanUpload(ctx, reader, project) {
		t.Error("expected an editor grant to allow upload")
	}
}

// The reason the role moved onto the grant: one group, two projects, two roles.
func TestOneGroupCanHoldDifferentRolesPerProject(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	reader := seedViewer(t, app, "reader")
	docs := seedProject(t, app, "docs", "Docs", false)
	handbook := seedProject(t, app, "handbook", "Handbook", false)

	group := &database.AccessGroup{Name: "engineering"}
	if err := app.handler.accessGroups.Create(ctx, group); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.accessGroups.AddMember(ctx, &database.AccessGroupMember{
		GroupID: group.ID, SubjectType: database.SubjectTypeUser, SubjectIdentifier: "reader",
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ slug, role string }{{"docs", "editor"}, {"handbook", "viewer"}} {
		form := url.Values{}
		form.Set("subject_kind", "group")
		form.Set("subject", "engineering")
		form.Set("role", tc.role)
		if resp := adminPost(t, app, cookies, "/admin/projects/"+tc.slug+"/grants", form); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("granting %s on %s: got %d", tc.role, tc.slug, resp.StatusCode)
		}
	}

	if !app.handler.resolver.CanUpload(ctx, reader, docs) {
		t.Error("expected editor on docs")
	}
	if app.handler.resolver.CanUpload(ctx, reader, handbook) {
		t.Error("expected viewer only on handbook")
	}
	if !app.handler.resolver.CanView(ctx, reader, handbook) {
		t.Error("expected viewer to be able to read handbook")
	}
}

// An org grant reaches every project in the org — the cascade that makes an
// org an access boundary.
func TestOrgGrantCascadesToItsProjects(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	reader := seedViewer(t, app, "reader")
	project := seedProject(t, app, "docs", "Docs", false)

	orgID := defaultOrgID(t, app)
	form := url.Values{}
	form.Set("subject_kind", "user")
	form.Set("subject", "reader")
	form.Set("role", "viewer")
	if resp := adminPost(t, app, cookies, "/admin/orgs/"+strconv.FormatInt(orgID, 10)+"/access/grant", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("granting on the org: got %d", resp.StatusCode)
	}

	if !app.handler.resolver.CanView(ctx, reader, project) {
		t.Error("expected an org grant to reach the org's projects")
	}
}

// Revoking must actually revoke, and a click that matches nothing must say so
// rather than redirecting as though it worked (issue #126).
func TestRevokeRemovesAccessAndReportsAMiss(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	reader := seedViewer(t, app, "reader")
	project := seedProject(t, app, "docs", "Docs", false)

	grantRole(t, app, project.ID, reader.ID, "viewer")
	grants, err := app.handler.accessGrants.ListByProject(ctx, project.ID)
	if err != nil || len(grants) != 1 {
		t.Fatalf("expected one grant, got %v (%v)", grants, err)
	}

	form := url.Values{}
	form.Set("grant_id", strconv.FormatInt(grants[0].ID, 10))
	if resp := adminPost(t, app, cookies, "/admin/projects/docs/grants/revoke", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoking: got %d", resp.StatusCode)
	}
	if app.handler.resolver.CanView(ctx, reader, project) {
		t.Error("expected the revoke to take effect")
	}

	resp := adminPost(t, app, cookies, "/admin/projects/docs/grants/revoke", form)
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "no+longer+exists") {
		t.Errorf("expected a revoke that matched nothing to say so, got %q", loc)
	}
}

// Exposure is what the edit form now saves, and 'authenticated' is the state
// the old four visibilities could not express.
func TestExposureAuthenticatedAdmitsAnySignedInUser(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	stranger := seedViewer(t, app, "stranger")
	project := seedProject(t, app, "docs", "Docs", false)

	if app.handler.resolver.CanView(ctx, stranger, project) {
		t.Fatal("a granted-only project must not admit a stranger")
	}

	form := url.Values{}
	form.Set("slug", "docs")
	form.Set("name", "Docs")
	form.Set("exposure", "authenticated")
	if resp := adminPost(t, app, cookies, "/admin/projects/docs/edit", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("saving exposure: got %d", resp.StatusCode)
	}

	updated, err := app.handler.projects.GetBySlug(ctx, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Exposure != database.ExposureAuthenticated {
		t.Fatalf("expected exposure to be saved, got %q", updated.Exposure)
	}
	if !app.handler.resolver.CanView(ctx, stranger, updated) {
		t.Error("expected any signed-in user to reach an authenticated-exposure project")
	}
	if app.handler.resolver.CanView(ctx, nil, updated) {
		t.Error("expected a signed-out visitor to be refused")
	}
}

// Renaming a group must not disturb what it grants: grants point at its id.
func TestRenamingAGroupKeepsItsGrants(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	reader := seedViewer(t, app, "reader")
	project := seedProject(t, app, "docs", "Docs", false)

	group := &database.AccessGroup{Name: "engineering"}
	if err := app.handler.accessGroups.Create(ctx, group); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.accessGroups.AddMember(ctx, &database.AccessGroupMember{
		GroupID: group.ID, SubjectType: database.SubjectTypeUser, SubjectIdentifier: "reader",
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.accessGrants.Grant(ctx, &database.AccessGrant{
		GroupID: &group.ID, ProjectID: &project.ID, Role: database.GrantRoleViewer,
	}); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("name", "platform")
	form.Set("description", "Renamed")
	if resp := adminPost(t, app, cookies, "/admin/access-groups/"+strconv.FormatInt(group.ID, 10)+"/edit", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("renaming: got %d", resp.StatusCode)
	}

	if !app.handler.resolver.CanView(ctx, reader, project) {
		t.Error("renaming a group must not revoke what it grants")
	}
}

// Deleting a group revokes what it granted, rather than leaving an orphan row
// that could later match a reused id.
func TestDeletingAGroupRevokesItsGrants(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	reader := seedViewer(t, app, "reader")
	project := seedProject(t, app, "docs", "Docs", false)

	group := &database.AccessGroup{Name: "engineering"}
	if err := app.handler.accessGroups.Create(ctx, group); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.accessGroups.AddMember(ctx, &database.AccessGroupMember{
		GroupID: group.ID, SubjectType: database.SubjectTypeUser, SubjectIdentifier: "reader",
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.accessGrants.Grant(ctx, &database.AccessGrant{
		GroupID: &group.ID, ProjectID: &project.ID, Role: database.GrantRoleViewer,
	}); err != nil {
		t.Fatal(err)
	}

	if resp := adminPost(t, app, cookies, "/admin/access-groups/"+strconv.FormatInt(group.ID, 10)+"/delete", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("deleting: got %d", resp.StatusCode)
	}

	if app.handler.resolver.CanView(ctx, reader, project) {
		t.Error("expected deleting the group to revoke the access it granted")
	}
	remaining, err := app.handler.accessGrants.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected the group's grants to be gone, got %d", len(remaining))
	}
}

// An org that still holds projects cannot be deleted out from under them, and
// the refusal explains itself.
func TestDeletingAnOrgWithProjectsIsRefused(t *testing.T) {
	app := setupTestApp(t)

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	seedProject(t, app, "docs", "Docs", false)

	orgID := defaultOrgID(t, app)
	resp := adminPost(t, app, cookies, "/admin/orgs/"+strconv.FormatInt(orgID, 10)+"/delete", url.Values{})
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "still+holds") {
		t.Errorf("expected the refusal to explain itself, got %q", loc)
	}
}

// Template errors only surface when a page is rendered, so both new admin
// pages get an explicit render check with content on them.
func TestNewAdminPagesRender(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	seedProject(t, app, "docs", "Docs", false)

	group := &database.AccessGroup{Name: "engineering", Description: "Dev team"}
	if err := app.handler.accessGroups.Create(ctx, group); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.accessGroups.AddMember(ctx, &database.AccessGroupMember{
		GroupID: group.ID, SubjectType: database.SubjectTypeLDAPGroup, SubjectIdentifier: "cn=eng,dc=example,dc=com",
	}); err != nil {
		t.Fatal(err)
	}
	orgID := defaultOrgID(t, app)
	if err := app.handler.accessGrants.Grant(ctx, &database.AccessGrant{
		GroupID: &group.ID, OrgID: &orgID, Role: database.GrantRoleViewer,
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ path, wants string }{
		{"/admin/access-groups", "cn=eng,dc=example,dc=com"},
		{"/admin/orgs", "engineering"},
		{"/admin/projects/docs/edit", "Who can reach this project"},
	} {
		status, body := adminGet(t, app, cookies, tc.path)
		if status != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", tc.path, status)
			continue
		}
		if !strings.Contains(body, tc.wants) {
			t.Errorf("%s: expected the page to show %q", tc.path, tc.wants)
		}
		// A template that fails mid-render leaves a truncated page rather than
		// an error status, so check the layout actually closed.
		if !strings.Contains(body, "</html>") {
			t.Errorf("%s: page looks truncated — a template probably failed mid-render", tc.path)
		}
	}
}

// The filter appears once a list is long enough to want narrowing, and not
// before: a filter box over a single card is furniture.
func TestAdminListsOfferAFilterOnlyWhenWorthIt(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	// One organization (the default) and one group: no filters yet.
	if err := app.handler.accessGroups.Create(ctx, &database.AccessGroup{Name: "engineering"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/orgs", "/admin/access-groups"} {
		if _, body := adminGet(t, app, cookies, path); strings.Contains(body, `class="admin-filter"`) {
			t.Errorf("%s: expected no filter for a single item", path)
		}
	}

	// A second of each, and both pages offer one.
	if err := app.handler.orgs.Create(ctx, &database.Org{Slug: "platform", Name: "Platform Team"}); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.accessGroups.Create(ctx, &database.AccessGroup{Name: "writers"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/orgs", "/admin/access-groups"} {
		_, body := adminGet(t, app, cookies, path)
		if !strings.Contains(body, `class="admin-filter"`) {
			t.Errorf("%s: expected a filter once there is more than one item", path)
		}
		// The filter matches against text the card carries, so it has to be there.
		if !strings.Contains(body, "data-filter-text=") {
			t.Errorf("%s: expected cards to carry the text the filter matches on", path)
		}
	}
}

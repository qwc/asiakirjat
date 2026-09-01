package sql

import (
	"context"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/testutil"
)

func ptr(v int64) *int64 { return &v }

// grantFixture builds the pieces every grant test needs: an org, a project in
// it, and a user.
type grantFixture struct {
	db      *sqlx.DB
	orgs    *OrgStore
	groups  *AccessGroupStore
	grants  *AccessGrantStore
	org     *database.Org
	project *database.Project
	user    *database.User
}

func newGrantFixture(t *testing.T) *grantFixture {
	t.Helper()
	db := testutil.NewTestDB(t)
	ctx := context.Background()

	f := &grantFixture{
		db:     db,
		orgs:   NewOrgStore(db),
		groups: NewAccessGroupStore(db),
		grants: NewAccessGrantStore(db),
	}

	org, err := f.orgs.GetBySlug(ctx, database.DefaultOrgSlug)
	if err != nil {
		t.Fatalf("migration 016 must create the default org: %v", err)
	}
	f.org = org

	f.project = &database.Project{Slug: "docs", Name: "Docs", Visibility: "custom", Exposure: database.ExposureGranted, OrgID: &org.ID}
	if err := NewProjectStore(db).Create(ctx, f.project); err != nil {
		t.Fatal(err)
	}

	hash := "x"
	f.user = &database.User{Username: "alice", Email: "alice@example.com", Password: &hash, AuthSource: "builtin", Role: "viewer"}
	if err := NewUserStore(db).Create(ctx, f.user); err != nil {
		t.Fatal(err)
	}
	return f
}

// A grant naming the user directly is the simple case that must keep working:
// adding one person to one project should not require inventing a group.
func TestGrantToUserDirectly(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	g := &database.AccessGrant{UserID: &f.user.ID, ProjectID: &f.project.ID, Role: database.GrantRoleEditor}
	if err := f.grants.Grant(ctx, g); err != nil {
		t.Fatal(err)
	}

	got, err := f.grants.GrantsForUser(ctx, f.user.ID, f.user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if got.Projects[f.project.ID] != database.GrantRoleEditor {
		t.Errorf("expected editor on the project, got %q", got.Projects[f.project.ID])
	}
}

// A group member naming a username resolves at check time — no login needed.
// This is the property issue #135 found missing in the old global rules.
func TestGroupMemberByUsernameResolvesWithoutLogin(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	group := &database.AccessGroup{Name: "engineering"}
	if err := f.groups.Create(ctx, group); err != nil {
		t.Fatal(err)
	}
	// Deliberately different casing: identifiers arrive inconsistently.
	if err := f.groups.AddMember(ctx, &database.AccessGroupMember{
		GroupID: group.ID, SubjectType: database.SubjectTypeUser, SubjectIdentifier: "ALICE",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.grants.Grant(ctx, &database.AccessGrant{
		GroupID: &group.ID, ProjectID: &f.project.ID, Role: database.GrantRoleViewer,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := f.grants.GrantsForUser(ctx, f.user.ID, f.user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if got.Projects[f.project.ID] != database.GrantRoleViewer {
		t.Errorf("expected viewer via group membership, got %q", got.Projects[f.project.ID])
	}
}

// The reason the role moved off the membership: one group, two projects, two
// different roles. The old access_list_members.role could not express this.
func TestOneGroupDifferentRolePerProject(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	other := &database.Project{Slug: "handbook", Name: "Handbook", Visibility: "custom", Exposure: database.ExposureGranted, OrgID: &f.org.ID}
	if err := NewProjectStore(f.db).Create(ctx, other); err != nil {
		t.Fatal(err)
	}

	group := &database.AccessGroup{Name: "engineering"}
	if err := f.groups.Create(ctx, group); err != nil {
		t.Fatal(err)
	}
	if err := f.groups.AddMember(ctx, &database.AccessGroupMember{
		GroupID: group.ID, SubjectType: database.SubjectTypeUser, SubjectIdentifier: "alice",
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.grants.Grant(ctx, &database.AccessGrant{GroupID: &group.ID, ProjectID: &f.project.ID, Role: database.GrantRoleEditor}); err != nil {
		t.Fatal(err)
	}
	if err := f.grants.Grant(ctx, &database.AccessGrant{GroupID: &group.ID, ProjectID: &other.ID, Role: database.GrantRoleViewer}); err != nil {
		t.Fatal(err)
	}

	got, err := f.grants.GrantsForUser(ctx, f.user.ID, f.user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if got.Projects[f.project.ID] != database.GrantRoleEditor {
		t.Errorf("expected editor on docs, got %q", got.Projects[f.project.ID])
	}
	if got.Projects[other.ID] != database.GrantRoleViewer {
		t.Errorf("expected viewer on handbook, got %q", got.Projects[other.ID])
	}
}

// Org grants come back keyed separately; the checker cascades them.
func TestOrgGrantReportedSeparately(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	if err := f.grants.Grant(ctx, &database.AccessGrant{
		UserID: &f.user.ID, OrgID: &f.org.ID, Role: database.GrantRoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := f.grants.GrantsForUser(ctx, f.user.ID, f.user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if got.Orgs[f.org.ID] != database.GrantRoleAdmin {
		t.Errorf("expected admin on the org, got %q", got.Orgs[f.org.ID])
	}
	if len(got.Projects) != 0 {
		t.Errorf("org grants must not be reported as project grants: %v", got.Projects)
	}
}

// Reached by two routes, the stronger role wins.
func TestStrongestRoleWins(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	group := &database.AccessGroup{Name: "everyone"}
	if err := f.groups.Create(ctx, group); err != nil {
		t.Fatal(err)
	}
	if err := f.groups.AddMember(ctx, &database.AccessGroupMember{
		GroupID: group.ID, SubjectType: database.SubjectTypeUser, SubjectIdentifier: "alice",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.grants.Grant(ctx, &database.AccessGrant{GroupID: &group.ID, ProjectID: &f.project.ID, Role: database.GrantRoleViewer}); err != nil {
		t.Fatal(err)
	}
	if err := f.grants.Grant(ctx, &database.AccessGrant{UserID: &f.user.ID, ProjectID: &f.project.ID, Role: database.GrantRoleEditor}); err != nil {
		t.Fatal(err)
	}

	got, err := f.grants.GrantsForUser(ctx, f.user.ID, f.user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if got.Projects[f.project.ID] != database.GrantRoleEditor {
		t.Errorf("expected the stronger role to win, got %q", got.Projects[f.project.ID])
	}
}

// Granting twice for the same subject and scope updates the role in place.
func TestRepeatGrantUpdatesRole(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	first := &database.AccessGrant{UserID: &f.user.ID, ProjectID: &f.project.ID, Role: database.GrantRoleViewer}
	if err := f.grants.Grant(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := &database.AccessGrant{UserID: &f.user.ID, ProjectID: &f.project.ID, Role: database.GrantRoleAdmin}
	if err := f.grants.Grant(ctx, second); err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Errorf("expected the grant to be updated in place, got a new row %d != %d", second.ID, first.ID)
	}

	grants, err := f.grants.ListByProject(ctx, f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}
	if grants[0].Role != database.GrantRoleAdmin {
		t.Errorf("expected the role to be updated to admin, got %q", grants[0].Role)
	}
}

// A revoke that matches nothing must say so. Silently reporting success while
// the access remains is the exact shape of issue #126.
func TestRevokeReportsWhetherAnythingWent(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	g := &database.AccessGrant{UserID: &f.user.ID, ProjectID: &f.project.ID, Role: database.GrantRoleViewer}
	if err := f.grants.Grant(ctx, g); err != nil {
		t.Fatal(err)
	}

	removed, err := f.grants.Revoke(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Error("expected the revoke to report that a row went")
	}

	removed, err = f.grants.Revoke(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("expected a second revoke to report that nothing matched")
	}
}

// Malformed grants are refused before they reach the database, which also
// covers MySQL servers too old to enforce CHECK.
func TestMalformedGrantsRefused(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	cases := map[string]*database.AccessGrant{
		"no subject":   {ProjectID: &f.project.ID, Role: database.GrantRoleViewer},
		"two subjects": {UserID: &f.user.ID, GroupID: ptr(1), ProjectID: &f.project.ID, Role: database.GrantRoleViewer},
		"no scope":     {UserID: &f.user.ID, Role: database.GrantRoleViewer},
		"two scopes":   {UserID: &f.user.ID, ProjectID: &f.project.ID, OrgID: &f.org.ID, Role: database.GrantRoleViewer},
		"unknown role": {UserID: &f.user.ID, ProjectID: &f.project.ID, Role: "owner"},
		"empty role":   {UserID: &f.user.ID, ProjectID: &f.project.ID, Role: ""},
		"bogus source": {UserID: &f.user.ID, ProjectID: &f.project.ID, Role: database.GrantRoleViewer, Source: "ldap"},
	}
	for name, g := range cases {
		if err := f.grants.Grant(ctx, g); err == nil {
			t.Errorf("%s: expected the grant to be refused", name)
		}
	}
}

// Deleting a project must take its grants with it: an orphan grant that later
// matched a reused id would grant access to a different project.
func TestGrantsDieWithTheirProject(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	if err := f.grants.Grant(ctx, &database.AccessGrant{
		UserID: &f.user.ID, ProjectID: &f.project.ID, Role: database.GrantRoleViewer,
	}); err != nil {
		t.Fatal(err)
	}
	if err := NewProjectStore(f.db).Delete(ctx, f.project.ID); err != nil {
		t.Fatal(err)
	}

	got, err := f.grants.GrantsForUser(ctx, f.user.ID, f.user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Projects) != 0 {
		t.Errorf("expected the grant to be deleted with the project, got %v", got.Projects)
	}
}

// An org that still holds projects cannot be deleted out from under them.
func TestOrgWithProjectsRefusesDeletion(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()

	if err := f.orgs.Delete(ctx, f.org.ID); err == nil {
		t.Error("expected deleting an org that still holds projects to be refused")
	}
}

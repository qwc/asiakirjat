package access

import (
	"context"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
)

// mkList creates a named access list with the given members.
func mkList(t *testing.T, f *checkerFixture, name string, members ...database.AccessListMember) *database.AccessList {
	t.Helper()
	ctx := context.Background()

	list := &database.AccessList{Name: name}
	if err := f.lists.Create(ctx, list); err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		m.ListID = list.ID
		if err := f.lists.AddMember(ctx, &m); err != nil {
			t.Fatal(err)
		}
	}
	return list
}

func mkListProject(t *testing.T, f *checkerFixture, slug string, list *database.AccessList) *database.Project {
	t.Helper()
	p := &database.Project{
		Slug: slug, Name: slug,
		Visibility: database.VisibilityList, AccessListID: &list.ID,
	}
	if err := f.projs.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestListVisibilityAdmitsNamedUsers covers the membership shape issue #125
// asks for: a list holding an LDAP group plus individually named users. The
// named users are matched by username at check time — no login sync needed —
// while the group half waits for a grant.
func TestListVisibilityAdmitsNamedUsers(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	alice := mkUser(t, f, "al-alice", "viewer")
	bob := mkUser(t, f, "al-bob", "viewer")
	outsider := mkUser(t, f, "al-outsider", "viewer")

	list := mkList(t, f, "engineering",
		database.AccessListMember{SubjectType: "ldap_group", SubjectIdentifier: "cn=eng,dc=example,dc=com", Role: "editor"},
		database.AccessListMember{SubjectType: "user", SubjectIdentifier: alice.Username, Role: "editor"},
		database.AccessListMember{SubjectType: "user", SubjectIdentifier: bob.Username, Role: "viewer"},
	)
	project := mkListProject(t, f, "eng-docs", list)

	if !f.checker.CanView(ctx, alice, project) {
		t.Error("a user named in the list should be able to view the project")
	}
	if !f.checker.CanView(ctx, bob, project) {
		t.Error("a viewer named in the list should be able to view the project")
	}
	if f.checker.CanView(ctx, outsider, project) {
		t.Error("a user outside the list must not reach the project")
	}

	if !f.checker.CanUpload(ctx, alice, project) {
		t.Error("an editor named in the list should be able to upload")
	}
	if f.checker.CanUpload(ctx, bob, project) {
		t.Error("a viewer named in the list must not be able to upload")
	}
}

// TestListVisibilityAdmitsGroupMembersViaGrant covers the other half: group
// membership is only known at login, so the sync records a grant and the
// checker reads it.
func TestListVisibilityAdmitsGroupMembersViaGrant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	carol := mkUser(t, f, "al-carol", "viewer")
	list := mkList(t, f, "ops",
		database.AccessListMember{SubjectType: "ldap_group", SubjectIdentifier: "cn=ops,dc=example,dc=com", Role: "editor"},
	)
	project := mkListProject(t, f, "ops-docs", list)

	if f.checker.CanView(ctx, carol, project) {
		t.Fatal("a group member should not be admitted before the login sync grants it")
	}

	if err := f.lists.UpsertGrant(ctx, &database.AccessListGrant{
		ListID: list.ID, UserID: carol.ID, Role: "editor", Source: "ldap",
	}); err != nil {
		t.Fatal(err)
	}

	if !f.checker.CanView(ctx, carol, project) {
		t.Error("a granted group member should be able to view the project")
	}
	if !f.checker.CanUpload(ctx, carol, project) {
		t.Error("a granted editor should be able to upload")
	}

	// Losing the group revokes the access it conferred.
	if err := f.lists.DeleteGrantsBySource(ctx, carol.ID, "ldap"); err != nil {
		t.Fatal(err)
	}
	if f.checker.CanView(ctx, carol, project) {
		t.Error("access should end when the grant is removed")
	}
}

// TestListProjectWithoutListFailsClosed pins the fail-closed decision: if the
// list pointer is missing, the project admits nobody rather than falling back
// to a broader rule.
func TestListProjectWithoutListFailsClosed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	user := mkUser(t, f, "al-nolist", "viewer")
	admin := mkUser(t, f, "al-admin", "admin")

	orphan := &database.Project{Slug: "orphan", Name: "orphan", Visibility: database.VisibilityList}
	if err := f.projs.Create(ctx, orphan); err != nil {
		t.Fatal(err)
	}

	if f.checker.CanView(ctx, user, orphan) {
		t.Error("a list project with no list must admit nobody")
	}
	if !f.checker.CanView(ctx, admin, orphan) {
		t.Error("admins still see everything")
	}
}

// TestFilterAccessibleIncludesListProjects makes sure the project list view
// agrees with CanView — the two used to drift, which is why this package
// exists.
func TestFilterAccessibleIncludesListProjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	dave := mkUser(t, f, "al-dave", "viewer")
	list := mkList(t, f, "writers",
		database.AccessListMember{SubjectType: "user", SubjectIdentifier: "al-dave", Role: "viewer"},
	)
	mine := mkListProject(t, f, "mine", list)

	otherList := mkList(t, f, "others",
		database.AccessListMember{SubjectType: "user", SubjectIdentifier: "someone-else", Role: "viewer"},
	)
	theirs := mkListProject(t, f, "theirs", otherList)

	filtered := f.checker.FilterAccessible(ctx, dave, []database.Project{*mine, *theirs})
	if len(filtered) != 1 || filtered[0].Slug != "mine" {
		t.Errorf("expected only the list project dave belongs to, got %+v", filtered)
	}

	// And it agrees with CanView on both.
	if !f.checker.CanView(ctx, dave, mine) || f.checker.CanView(ctx, dave, theirs) {
		t.Error("FilterAccessible and CanView disagree about list projects")
	}
}

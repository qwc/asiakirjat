package sql

import (
	"context"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/testutil"
)

// TestAccessListMixedMembership is the shape issue #125 asks for: a list that
// is an LDAP group plus a couple of named users, reusable across projects.
func TestAccessListMixedMembership(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	store := NewAccessListStore(db)

	list := &database.AccessList{Name: "engineering", Description: "Dev team"}
	if err := store.Create(ctx, list); err != nil {
		t.Fatal(err)
	}
	if list.ID == 0 {
		t.Fatal("expected non-zero ID after create")
	}

	members := []database.AccessListMember{
		{ListID: list.ID, SubjectType: "ldap_group", SubjectIdentifier: "cn=eng,ou=groups,dc=example,dc=com", Role: "editor"},
		{ListID: list.ID, SubjectType: "user", SubjectIdentifier: "alice", Role: "editor"},
		{ListID: list.ID, SubjectType: "user", SubjectIdentifier: "bob", Role: "viewer"},
	}
	for _, m := range members {
		if err := store.AddMember(ctx, &m); err != nil {
			t.Fatalf("adding %s %q: %v", m.SubjectType, m.SubjectIdentifier, err)
		}
	}

	got, err := store.ListMembers(ctx, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 members, got %d", len(got))
	}

	// Re-adding a subject updates its role rather than duplicating it.
	if err := store.AddMember(ctx, &database.AccessListMember{
		ListID: list.ID, SubjectType: "user", SubjectIdentifier: "bob", Role: "editor",
	}); err != nil {
		t.Fatal(err)
	}
	got, err = store.ListMembers(ctx, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("re-adding a member should update in place, got %d members", len(got))
	}
	for _, m := range got {
		if m.SubjectIdentifier == "bob" && m.Role != "editor" {
			t.Errorf("expected bob's role updated to editor, got %q", m.Role)
		}
	}
}

// TestAccessListRejectsUnknownSubjectOrRole keeps rule values constrained at
// store entry rather than trusting the column default.
func TestAccessListRejectsUnknownSubjectOrRole(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	store := NewAccessListStore(db)

	list := &database.AccessList{Name: "constrained"}
	if err := store.Create(ctx, list); err != nil {
		t.Fatal(err)
	}

	if err := store.AddMember(ctx, &database.AccessListMember{
		ListID: list.ID, SubjectType: "everyone", SubjectIdentifier: "x", Role: "viewer",
	}); err == nil {
		t.Error("expected an unknown subject type to be rejected")
	}
	if err := store.AddMember(ctx, &database.AccessListMember{
		ListID: list.ID, SubjectType: "user", SubjectIdentifier: "x", Role: "admin",
	}); err == nil {
		t.Error("expected an unknown access role to be rejected")
	}
}

// TestAccessListNamesAreUnique — the name is what an admin picks in the
// visibility dropdown, so two lists cannot share one.
func TestAccessListNamesAreUnique(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	store := NewAccessListStore(db)

	if err := store.Create(ctx, &database.AccessList{Name: "dupe"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, &database.AccessList{Name: "dupe"}); err == nil {
		t.Error("expected a duplicate list name to be rejected")
	}
}

// TestDeleteAccessListInUseIsRefused covers the ON DELETE RESTRICT decision:
// deleting a list a project still points at would silently change who can
// reach that project, so the database refuses it and CountProjectsUsing lets
// callers say why.
func TestDeleteAccessListInUseIsRefused(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	store := NewAccessListStore(db)
	projects := NewProjectStore(db)

	list := &database.AccessList{Name: "in-use"}
	if err := store.Create(ctx, list); err != nil {
		t.Fatal(err)
	}
	project := &database.Project{
		Slug: "guarded", Name: "Guarded",
		Visibility: database.VisibilityList, AccessListID: &list.ID,
	}
	if err := projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	count, err := store.CountProjectsUsing(ctx, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 project using the list, got %d", count)
	}

	if err := store.Delete(ctx, list.ID); err == nil {
		t.Error("expected deleting a list still in use to be refused")
	}

	// Once nothing points at it, the delete goes through and takes the
	// members with it.
	project.Visibility = database.VisibilityCustom
	project.AccessListID = nil
	if err := projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, list.ID); err != nil {
		t.Errorf("expected delete to succeed once unused: %v", err)
	}
}

// TestProjectRoundTripsAccessList makes sure the new column survives a
// create/fetch/update cycle — the pointer is what the checker will read.
func TestProjectRoundTripsAccessList(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	store := NewAccessListStore(db)
	projects := NewProjectStore(db)

	list := &database.AccessList{Name: "round-trip"}
	if err := store.Create(ctx, list); err != nil {
		t.Fatal(err)
	}

	project := &database.Project{
		Slug: "rt", Name: "RT",
		Visibility: database.VisibilityList, AccessListID: &list.ID,
	}
	if err := projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	got, err := projects.GetBySlug(ctx, "rt")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessListID == nil || *got.AccessListID != list.ID {
		t.Fatalf("expected access_list_id %d to round trip, got %v", list.ID, got.AccessListID)
	}

	got.Visibility = database.VisibilityCustom
	got.AccessListID = nil
	if err := projects.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, err = projects.GetBySlug(ctx, "rt")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessListID != nil {
		t.Errorf("expected access_list_id cleared, got %v", *got.AccessListID)
	}
}

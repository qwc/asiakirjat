package sql

import (
	"context"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/testutil"
)

// seedAccessFixture creates one project and one user to hang grants on.
func seedAccessFixture(t *testing.T, db *sqlx.DB) (*database.Project, *database.User) {
	t.Helper()
	ctx := context.Background()

	pStore := NewProjectStore(db)
	uStore := NewUserStore(db)

	project := &database.Project{
		Slug: "multi-source", Name: "Multi Source",
		Visibility: database.VisibilityCustom,
	}
	if err := pStore.Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	user := &database.User{
		Username: "multi", Email: "multi@example.com",
		AuthSource: "ldap", Role: "viewer",
	}
	if err := uStore.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	return project, user
}

// TestGrantAllowsOneRowPerSource is the behaviour issue #133 blocks: a user
// who was granted access by hand and then matches an LDAP group mapping must
// end up with both grants recorded, not an error. The unique key is meant to
// be (project_id, user_id, source).
func TestGrantAllowsOneRowPerSource(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	aStore := NewProjectAccessStore(db)

	project, user := seedAccessFixture(t, db)

	for _, source := range []string{"manual", "ldap"} {
		if err := aStore.Grant(ctx, &database.ProjectAccess{
			ProjectID: project.ID, UserID: user.ID, Role: "viewer", Source: source,
		}); err != nil {
			t.Fatalf("granting %s access: %v", source, err)
		}
	}

	rows, err := aStore.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected a manual and an ldap grant, got %d row(s): %+v", len(rows), rows)
	}
}

// TestRevokeBySourceLeavesOtherSources covers the other half: removing the
// manual grant must not disturb a grant the LDAP sync owns, and vice versa.
func TestRevokeBySourceLeavesOtherSources(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	aStore := NewProjectAccessStore(db)

	project, user := seedAccessFixture(t, db)

	for _, source := range []string{"manual", "ldap"} {
		if err := aStore.Grant(ctx, &database.ProjectAccess{
			ProjectID: project.ID, UserID: user.ID, Role: "viewer", Source: source,
		}); err != nil {
			t.Fatalf("granting %s access: %v", source, err)
		}
	}

	if err := aStore.RevokeBySource(ctx, project.ID, user.ID, "manual"); err != nil {
		t.Fatal(err)
	}

	rows, err := aStore.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected the ldap grant to survive, got %d row(s): %+v", len(rows), rows)
	}
	if rows[0].Source != "ldap" {
		t.Errorf("expected surviving grant to be ldap-sourced, got %q", rows[0].Source)
	}
}

// TestGrantUpdatesRoleWithinSource guards the upsert path that the new unique
// key backs: re-granting the same source changes the role in place instead of
// inserting a duplicate.
func TestGrantUpdatesRoleWithinSource(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	aStore := NewProjectAccessStore(db)

	project, user := seedAccessFixture(t, db)

	if err := aStore.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID, UserID: user.ID, Role: "viewer", Source: "ldap",
	}); err != nil {
		t.Fatal(err)
	}
	if err := aStore.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID, UserID: user.ID, Role: "editor", Source: "ldap",
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := aStore.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected the ldap grant to be updated in place, got %d row(s)", len(rows))
	}
	if rows[0].Role != "editor" {
		t.Errorf("expected role editor after re-grant, got %q", rows[0].Role)
	}
}

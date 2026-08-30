package sql

import (
	"context"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/testutil"
)

// TestGrantRejectsInvalidRoleAndSource covers audit items L-1 and L-7: a grant
// decides who reaches a project, so a role or source the application does not
// recognise should fail loudly rather than write a row that nothing matches
// and no one can see.
func TestGrantRejectsInvalidRoleAndSource(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	store := NewProjectAccessStore(db)
	project, user := seedAccessFixture(t, db)

	cases := []struct {
		name   string
		role   string
		source string
	}{
		{"role admin is not a grant", "admin", "manual"},
		{"unknown role", "superuser", "manual"},
		{"empty role", "", "manual"},
		{"unknown source", "viewer", "sso"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.Grant(ctx, &database.ProjectAccess{
				ProjectID: project.ID, UserID: user.ID, Role: tc.role, Source: tc.source,
			})
			if err == nil {
				t.Errorf("expected role %q / source %q to be rejected", tc.role, tc.source)
			}
		})
	}

	// The valid combination still works, and an empty source still defaults.
	if err := store.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID, UserID: user.ID, Role: "editor",
	}); err != nil {
		t.Fatalf("expected a valid grant to succeed: %v", err)
	}
	rows, err := store.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Source != database.AccessSourceManual {
		t.Errorf("expected one manual grant, got %+v", rows)
	}
}

// TestGlobalAccessRejectsInvalidValues — same rule for the org-wide list.
func TestGlobalAccessRejectsInvalidValues(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	store := NewGlobalAccessStore(db)
	_, user := seedAccessFixture(t, db)

	if err := store.CreateRule(ctx, &database.GlobalAccess{
		SubjectType: "user", SubjectIdentifier: "alice", Role: "admin",
	}); err == nil {
		t.Error("expected an admin global access rule to be rejected")
	}
	if err := store.CreateRule(ctx, &database.GlobalAccess{
		SubjectType: "everyone", SubjectIdentifier: "*", Role: "viewer",
	}); err == nil {
		t.Error("expected an unknown subject type to be rejected")
	}
	if err := store.UpsertGrant(ctx, &database.GlobalAccessGrant{
		UserID: user.ID, Role: "admin", Source: "ldap",
	}); err == nil {
		t.Error("expected an admin global access grant to be rejected")
	}

	if err := store.CreateRule(ctx, &database.GlobalAccess{
		SubjectType: "ldap_group", SubjectIdentifier: "cn=eng", Role: "editor",
	}); err != nil {
		t.Errorf("expected a valid rule to succeed: %v", err)
	}
}

// TestGroupMappingRejectsInvalidRole — and for group mappings, which is how a
// role reaches project_access on every LDAP/OAuth2 login.
func TestGroupMappingRejectsInvalidRole(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	store := NewAuthGroupMappingStore(db)
	project, _ := seedAccessFixture(t, db)

	if err := store.Create(ctx, &database.AuthGroupMapping{
		AuthSource: "ldap", GroupIdentifier: "cn=eng", ProjectID: project.ID, Role: "admin",
	}); err == nil {
		t.Error("expected an admin group mapping to be rejected")
	}
	if err := store.Create(ctx, &database.AuthGroupMapping{
		AuthSource: "ldap", GroupIdentifier: "cn=eng", ProjectID: project.ID, Role: "editor",
	}); err != nil {
		t.Errorf("expected a valid mapping to succeed: %v", err)
	}
}

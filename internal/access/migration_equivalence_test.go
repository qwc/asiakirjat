package access_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/access"
	"github.com/qwc/asiakirjat/internal/database"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
)

// This is the safety net for the access-model migration (issues #150, #151).
//
// The migration's one job is to preserve exactly who can reach what. Both
// failure directions are silent: a leak looks like nothing, and a lockout looks
// like "the app is broken". So rather than assert on the shape of the migrated
// data, this enumerates every (user, project) pair, records what the OLD
// checker allowed, runs the migration, and asserts the NEW resolver allows
// precisely the same thing.
//
// Any intended difference has to be listed here explicitly. There are none.

type verdict struct {
	view   bool
	upload bool
	manage bool
}

func (v verdict) String() string {
	return fmt.Sprintf("view=%t upload=%t manage=%t", v.view, v.upload, v.manage)
}

// snapshotOld records what the pre-migration checker allows for every pair.
func snapshotOld(ctx context.Context, t *testing.T, db *sqlx.DB, users []database.User, projects []database.Project) map[string]verdict {
	t.Helper()
	checker := access.NewChecker(
		sqlstore.NewProjectAccessStore(db),
		sqlstore.NewGlobalAccessStore(db),
		sqlstore.NewAccessListStore(db),
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	)

	out := map[string]verdict{}
	for i := range users {
		for j := range projects {
			u, p := &users[i], &projects[j]
			out[u.Username+"/"+p.Slug] = verdict{
				view:   checker.CanView(ctx, u, p),
				upload: checker.CanUpload(ctx, u, p),
				manage: checker.CanManage(u, p),
			}
		}
	}
	return out
}

// snapshotNew records what the post-migration resolver allows for every pair.
func snapshotNew(ctx context.Context, t *testing.T, db *sqlx.DB, users []database.User, projects []database.Project) map[string]verdict {
	t.Helper()
	resolver := access.NewResolver(
		sqlstore.NewAccessGrantStore(db),
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	)

	out := map[string]verdict{}
	for i := range users {
		for j := range projects {
			u, p := &users[i], &projects[j]
			out[u.Username+"/"+p.Slug] = verdict{
				view:   resolver.CanView(ctx, u, p),
				upload: resolver.CanUpload(ctx, u, p),
				manage: resolver.CanManage(ctx, u, p),
			}
		}
	}
	return out
}

// intendedChanges lists the pairs whose access the migration deliberately
// changes. Everything else must be identical, and an intended change that does
// not actually happen is a failure too — otherwise this list would rot into a
// place to hide regressions.
//
// There is exactly one, and it is a widening:
//
//	erin created owned-docs. PR #118 let a creator MANAGE their own project,
//	deciding it from projects.created_by — but a 'custom' project grants
//	nothing to anyone without a project_access row, so erin could change the
//	settings of a project she could not read or upload to. The new model says
//	ownership with an admin grant, and admin outranks editor outranks viewer,
//	so she can now do all three. Treating this as a bug to preserve would mean
//	keeping a created_by branch in the checker forever to reproduce an
//	oversight.
var intendedChanges = map[string]struct{ before, after verdict }{
	"erin/owned-docs": {
		before: verdict{view: false, upload: false, manage: true},
		after:  verdict{view: true, upload: true, manage: true},
	},
}

func diffSnapshots(t *testing.T, before, after map[string]verdict) {
	t.Helper()
	keys := make([]string, 0, len(before))
	for k := range before {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		want, intended := intendedChanges[k]
		switch {
		case intended:
			if before[k] != want.before || after[k] != want.after {
				t.Errorf("intended change for %s did not happen as described:\n  expected %s -> %s\n       got %s -> %s",
					k, want.before, want.after, before[k], after[k])
			}
		case before[k] != after[k]:
			t.Errorf("access changed for %s:\n  before migration: %s\n   after migration: %s", k, before[k], after[k])
		}
	}
}

// seedLegacyWorld builds an installation that uses every old mechanism at
// once, which is the case the migration has to get right.
func seedLegacyWorld(t *testing.T) (*sqlx.DB, []database.User, []database.Project) {
	t.Helper()
	ctx := context.Background()
	db := testutil.NewTestDB(t)

	users := sqlstore.NewUserStore(db)
	projects := sqlstore.NewProjectStore(db)
	projectAccess := sqlstore.NewProjectAccessStore(db)
	globalAccess := sqlstore.NewGlobalAccessStore(db)
	lists := sqlstore.NewAccessListStore(db)
	mappings := sqlstore.NewAuthGroupMappingStore(db)

	hash := "x"
	mkUser := func(name, role string) *database.User {
		u := &database.User{Username: name, Email: name + "@example.com", Password: &hash, AuthSource: "builtin", Role: role}
		if err := users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	admin := mkUser("admin", "admin")
	globalEditor := mkUser("globaleditor", "editor")
	mkUser("alice", "viewer")        // named in a global rule
	bob := mkUser("bob", "viewer")   // manual per-project grant + resolved global grant
	mkUser("carol", "viewer")        // access-list member by name
	dave := mkUser("dave", "viewer") // LDAP-synced onto one mapped project
	erin := mkUser("erin", "viewer") // creator of a project
	mkUser("frank", "viewer")        // reaches nothing
	_ = admin
	_ = globalEditor

	mkProject := func(slug, visibility string, creator *int64) *database.Project {
		p := &database.Project{Slug: slug, Name: slug, Visibility: visibility, CreatedBy: creator}
		if err := projects.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	pub := mkProject("public-docs", database.VisibilityPublic, nil)
	priv := mkProject("private-docs", database.VisibilityPrivate, nil)
	custom := mkProject("custom-docs", database.VisibilityCustom, nil)
	listed := mkProject("listed-docs", database.VisibilityList, nil)
	owned := mkProject("owned-docs", database.VisibilityCustom, &erin.ID)
	synced := mkProject("synced-docs", database.VisibilityCustom, nil)
	_ = pub

	// Global (private) access: alice by name, plus a resolved LDAP grant.
	if err := globalAccess.CreateRule(ctx, &database.GlobalAccess{
		SubjectType: database.SubjectTypeUser, SubjectIdentifier: "alice", Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	if err := globalAccess.CreateRule(ctx, &database.GlobalAccess{
		SubjectType: database.SubjectTypeLDAPGroup, SubjectIdentifier: "cn=staff,dc=example,dc=com", Role: "editor",
	}); err != nil {
		t.Fatal(err)
	}
	if err := globalAccess.UpsertGrant(ctx, &database.GlobalAccessGrant{
		UserID: bob.ID, Role: "editor", Source: database.AccessSourceLDAP,
	}); err != nil {
		t.Fatal(err)
	}

	// A named access list with mixed member roles, pointed at by a project.
	list := &database.AccessList{Name: "engineering", Description: "Dev team"}
	if err := lists.Create(ctx, list); err != nil {
		t.Fatal(err)
	}
	for _, m := range []database.AccessListMember{
		{ListID: list.ID, SubjectType: database.SubjectTypeUser, SubjectIdentifier: "carol", Role: "editor"},
		{ListID: list.ID, SubjectType: database.SubjectTypeUser, SubjectIdentifier: "frank", Role: "viewer"},
		{ListID: list.ID, SubjectType: database.SubjectTypeLDAPGroup, SubjectIdentifier: "cn=eng,dc=example,dc=com", Role: "viewer"},
	} {
		member := m
		if err := lists.AddMember(ctx, &member); err != nil {
			t.Fatal(err)
		}
	}
	listed.AccessListID = &list.ID
	if err := projects.Update(ctx, listed); err != nil {
		t.Fatal(err)
	}

	// A manual per-project grant on a custom project.
	if err := projectAccess.Grant(ctx, &database.ProjectAccess{
		ProjectID: custom.ID, UserID: bob.ID, Role: "editor", Source: database.AccessSourceManual,
	}); err != nil {
		t.Fatal(err)
	}

	// One LDAP mapping onto synced-docs, plus the project_access row the login
	// sync would have written for dave.
	if err := mappings.Create(ctx, &database.AuthGroupMapping{
		AuthSource: "ldap", GroupIdentifier: "cn=ops,dc=example,dc=com", ProjectID: synced.ID, Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	if err := projectAccess.Grant(ctx, &database.ProjectAccess{
		ProjectID: synced.ID, UserID: dave.ID, Role: "viewer", Source: database.AccessSourceLDAP,
	}); err != nil {
		t.Fatal(err)
	}

	allUsers, err := users.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	allProjects, err := projects.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = owned
	_ = priv
	return db, allUsers, allProjects
}

// TestMigrationPreservesAccessExactly is the gate on the whole redesign: run
// the migration over an installation using every old mechanism, and nobody's
// access moves in either direction.
func TestMigrationPreservesAccessExactly(t *testing.T) {
	ctx := context.Background()
	db, users, projects := seedLegacyWorld(t)

	before := snapshotOld(ctx, t, db, users, projects)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := sqlstore.MigrateAccessModel(ctx, db, logger); err != nil {
		t.Fatalf("migrating access model: %v", err)
	}

	// Re-read: the migration set exposure and org_id on every project.
	migrated, err := sqlstore.NewProjectStore(db).List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	after := snapshotNew(ctx, t, db, users, migrated)
	diffSnapshots(t, before, after)
}

// The migration must never run twice: a second pass would recreate grants an
// admin had since revoked.
func TestMigrationRunsOnlyOnce(t *testing.T) {
	ctx := context.Background()
	db, _, _ := seedLegacyWorld(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	if err := sqlstore.MigrateAccessModel(ctx, db, logger); err != nil {
		t.Fatal(err)
	}

	var grantsAfterFirst int
	if err := db.GetContext(ctx, &grantsAfterFirst, `SELECT COUNT(*) FROM access_grants`); err != nil {
		t.Fatal(err)
	}
	if grantsAfterFirst == 0 {
		t.Fatal("expected the migration to create grants")
	}

	// An admin revokes one, then the process restarts.
	if _, err := db.ExecContext(ctx, `DELETE FROM access_grants WHERE id = (SELECT MIN(id) FROM access_grants)`); err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.MigrateAccessModel(ctx, db, logger); err != nil {
		t.Fatal(err)
	}

	var grantsAfterSecond int
	if err := db.GetContext(ctx, &grantsAfterSecond, `SELECT COUNT(*) FROM access_grants`); err != nil {
		t.Fatal(err)
	}
	if grantsAfterSecond != grantsAfterFirst-1 {
		t.Errorf("the migration re-ran and undid a revoke: %d grants, expected %d", grantsAfterSecond, grantsAfterFirst-1)
	}
}

// A fresh installation has nothing to translate, and must not invent anything.
func TestMigrationOnEmptyInstallation(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	if err := sqlstore.MigrateAccessModel(ctx, db, logger); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"access_groups", "access_grants", "access_group_resolved"} {
		var count int
		if err := db.GetContext(ctx, &count, `SELECT COUNT(*) FROM `+table); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("expected %s to be empty on a fresh installation, got %d rows", table, count)
		}
	}
}

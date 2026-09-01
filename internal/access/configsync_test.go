package access_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/access"
	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
)

type configWorld struct {
	db      *sqlx.DB
	sync    *access.ConfigSync
	res     *access.Resolver
	groups  *sqlstore.AccessGroupStore
	grants  *sqlstore.AccessGrantStore
	user    *database.User
	project *database.Project
	orgSlug string
}

func newConfigWorld(t *testing.T) *configWorld {
	t.Helper()
	ctx := context.Background()
	db := testutil.NewTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	groups := sqlstore.NewAccessGroupStore(db)
	grants := sqlstore.NewAccessGrantStore(db)
	orgs := sqlstore.NewOrgStore(db)
	projects := sqlstore.NewProjectStore(db)
	users := sqlstore.NewUserStore(db)

	hash := "x"
	user := &database.User{Username: "alice", Email: "alice@example.com", Password: &hash, AuthSource: "builtin", Role: "viewer"}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	project := &database.Project{Slug: "docs", Name: "Docs", Visibility: "custom", Exposure: database.ExposureGranted}
	if err := projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	return &configWorld{
		db:      db,
		sync:    access.NewConfigSync(groups, grants, orgs, projects, users, logger),
		res:     access.NewResolver(grants, logger),
		groups:  groups,
		grants:  grants,
		user:    user,
		project: project,
		orgSlug: "default",
	}
}

func cfgWith(groups []config.AccessGroupConfig, grants []config.AccessGrantConfig) *config.Config {
	cfg := config.Defaults()
	cfg.Access.Groups = groups
	cfg.Access.Grants = grants
	return &cfg
}

// The declared shape works end to end: a group with a user member, granted on
// a project, and the resolver lets that user in.
func TestConfigDeclaresGroupsAndGrants(t *testing.T) {
	ctx := context.Background()
	w := newConfigWorld(t)

	cfg := cfgWith(
		[]config.AccessGroupConfig{{
			Name:    "engineering",
			Members: []config.AccessGroupMemberConfig{{User: "alice"}},
		}},
		[]config.AccessGrantConfig{{Group: "engineering", Project: "docs", Role: "editor"}},
	)
	if err := w.sync.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	if !w.res.CanUpload(ctx, w.user, w.project) {
		t.Error("expected the declared grant to apply")
	}
}

// The property that makes config declarative rather than additive: deleting a
// line revokes. An entry that stays in the database with nothing to say so is
// the failure this whole redesign is about.
func TestRemovingAGrantFromConfigRevokesIt(t *testing.T) {
	ctx := context.Background()
	w := newConfigWorld(t)

	full := cfgWith(
		[]config.AccessGroupConfig{{Name: "engineering", Members: []config.AccessGroupMemberConfig{{User: "alice"}}}},
		[]config.AccessGrantConfig{{Group: "engineering", Project: "docs", Role: "editor"}},
	)
	if err := w.sync.Apply(ctx, full); err != nil {
		t.Fatal(err)
	}
	if !w.res.CanView(ctx, w.user, w.project) {
		t.Fatal("expected the grant to apply first")
	}

	// The operator deletes the grant from config.yaml and restarts.
	trimmed := cfgWith(full.Access.Groups, nil)
	if err := w.sync.Apply(ctx, trimmed); err != nil {
		t.Fatal(err)
	}
	if w.res.CanView(ctx, w.user, w.project) {
		t.Error("removing a grant from config must revoke it")
	}
}

// The same for membership: dropping a member from config removes them.
func TestRemovingAMemberFromConfigRemovesIt(t *testing.T) {
	ctx := context.Background()
	w := newConfigWorld(t)

	full := cfgWith(
		[]config.AccessGroupConfig{{Name: "engineering", Members: []config.AccessGroupMemberConfig{{User: "alice"}}}},
		[]config.AccessGrantConfig{{Group: "engineering", Project: "docs", Role: "viewer"}},
	)
	if err := w.sync.Apply(ctx, full); err != nil {
		t.Fatal(err)
	}

	empty := cfgWith([]config.AccessGroupConfig{{Name: "engineering"}}, full.Access.Grants)
	if err := w.sync.Apply(ctx, empty); err != nil {
		t.Fatal(err)
	}
	if w.res.CanView(ctx, w.user, w.project) {
		t.Error("removing a member from config must remove their access")
	}
}

// What the admin did in the UI is not config's to undo.
func TestConfigSyncLeavesManualAccessAlone(t *testing.T) {
	ctx := context.Background()
	w := newConfigWorld(t)

	// An admin grants alice directly, in the UI.
	if err := w.grants.Grant(ctx, &database.AccessGrant{
		UserID: &w.user.ID, ProjectID: &w.project.ID, Role: database.GrantRoleViewer,
	}); err != nil {
		t.Fatal(err)
	}
	// And adds a member to a group config also manages.
	group := &database.AccessGroup{Name: "engineering"}
	if err := w.groups.Create(ctx, group); err != nil {
		t.Fatal(err)
	}
	if err := w.groups.AddMember(ctx, &database.AccessGroupMember{
		GroupID: group.ID, SubjectType: database.SubjectTypeUser, SubjectIdentifier: "manual-person",
	}); err != nil {
		t.Fatal(err)
	}

	// Config declares the group but neither of those things.
	if err := w.sync.Apply(ctx, cfgWith(
		[]config.AccessGroupConfig{{Name: "engineering", Members: []config.AccessGroupMemberConfig{{LDAPGroup: "cn=eng"}}}},
		nil,
	)); err != nil {
		t.Fatal(err)
	}

	if !w.res.CanView(ctx, w.user, w.project) {
		t.Error("config must not revoke a grant an admin made in the UI")
	}
	members, err := w.groups.ListMembers(ctx, group.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kept bool
	for _, m := range members {
		if m.SubjectIdentifier == "manual-person" {
			kept = true
		}
	}
	if !kept {
		t.Error("config must not remove a member an admin added in the UI")
	}
}

// The retired auth.*.project_groups keys still apply, translated, rather than
// silently doing nothing on an installation that has not moved them yet.
func TestRetiredProjectGroupsStillApply(t *testing.T) {
	ctx := context.Background()
	w := newConfigWorld(t)

	cfg := config.Defaults()
	cfg.Auth.LDAP.ProjectGroups = []config.AuthGroupMapping{
		{Group: "cn=eng,dc=example,dc=com", Project: "docs", Role: "editor"},
	}
	if err := w.sync.Apply(ctx, &cfg); err != nil {
		t.Fatal(err)
	}

	group, err := w.groups.GetByName(ctx, "cn=eng,dc=example,dc=com")
	if err != nil {
		t.Fatalf("expected the mapping to become an access group: %v", err)
	}
	members, err := w.groups.ListMembers(ctx, group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].SubjectType != database.SubjectTypeLDAPGroup {
		t.Fatalf("expected one ldap_group member, got %v", members)
	}
	grants, err := w.grants.ListByProject(ctx, w.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].Role != database.GrantRoleEditor {
		t.Errorf("expected an editor grant on the project, got %v", grants)
	}
}

// A typo must not take the server down, and must not be mistaken for a
// declaration that then survives the removal pass.
func TestBadEntriesAreSkippedNotFatal(t *testing.T) {
	ctx := context.Background()
	w := newConfigWorld(t)

	cfg := cfgWith(
		[]config.AccessGroupConfig{{Name: "engineering", Members: []config.AccessGroupMemberConfig{{User: "alice"}}}},
		[]config.AccessGrantConfig{
			{Group: "engineering", Project: "nonexistent", Role: "editor"},
			{Group: "nonexistent", Project: "docs", Role: "editor"},
			{Group: "engineering", Project: "docs", Role: "wizard"},
			{Group: "engineering", User: "alice", Project: "docs", Role: "editor"},
			{Group: "engineering", Org: "default", Project: "docs", Role: "editor"},
			{Group: "engineering", Project: "docs", Role: "viewer"}, // the only good one
		},
	)
	if err := w.sync.Apply(ctx, cfg); err != nil {
		t.Fatalf("bad entries must not fail the sync: %v", err)
	}

	grants, err := w.grants.ListByProject(ctx, w.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 || grants[0].Role != database.GrantRoleViewer {
		t.Errorf("expected only the valid grant to land, got %v", grants)
	}
}

// Applying the same config twice changes nothing — a restart is not an event.
func TestConfigSyncIsIdempotent(t *testing.T) {
	ctx := context.Background()
	w := newConfigWorld(t)

	cfg := cfgWith(
		[]config.AccessGroupConfig{{Name: "engineering", Members: []config.AccessGroupMemberConfig{{User: "alice"}}}},
		[]config.AccessGrantConfig{{Group: "engineering", Project: "docs", Role: "editor"}},
	)
	for i := 0; i < 3; i++ {
		if err := w.sync.Apply(ctx, cfg); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	grants, err := w.grants.ListByProject(ctx, w.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 {
		t.Errorf("expected one grant after three runs, got %d", len(grants))
	}
	group, err := w.groups.GetByName(ctx, "engineering")
	if err != nil {
		t.Fatal(err)
	}
	members, err := w.groups.ListMembers(ctx, group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 {
		t.Errorf("expected one member after three runs, got %d", len(members))
	}
	if !w.res.CanUpload(ctx, w.user, w.project) {
		t.Error("expected access to survive repeated syncs")
	}
}

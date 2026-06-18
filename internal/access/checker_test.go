package access

import (
	"context"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
)

// newCheckerFromDB returns a Checker plus the stores it talks to so tests
// can seed users / projects / grants before exercising the rules.
type checkerFixture struct {
	checker *Checker
	users   *sqlstore.UserStore
	projs   *sqlstore.ProjectStore
	access  *sqlstore.ProjectAccessStore
	global  *sqlstore.GlobalAccessStore
}

func newFixture(t *testing.T) *checkerFixture {
	t.Helper()
	db := testutil.NewTestDB(t)
	users := sqlstore.NewUserStore(db)
	projs := sqlstore.NewProjectStore(db)
	access := sqlstore.NewProjectAccessStore(db)
	global := sqlstore.NewGlobalAccessStore(db)
	return &checkerFixture{
		checker: NewChecker(access, global, testutil.TestLogger()),
		users:   users,
		projs:   projs,
		access:  access,
		global:  global,
	}
}

func mkUser(t *testing.T, f *checkerFixture, name, role string) *database.User {
	t.Helper()
	u := &database.User{Username: name, AuthSource: "builtin", Role: role}
	if err := f.users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func mkProject(t *testing.T, f *checkerFixture, slug, visibility string) *database.Project {
	t.Helper()
	p := &database.Project{Slug: slug, Name: slug, Visibility: visibility}
	if err := f.projs.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

// CanView: every combination of visibility × user role × grant.
// Behavior matches handler's pre-refactor canViewProject — this test locks it in.
func TestCanView(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	admin := mkUser(t, f, "ck-admin", "admin")
	editor := mkUser(t, f, "ck-editor", "editor")
	viewer := mkUser(t, f, "ck-viewer", "viewer")
	grantedViewer := mkUser(t, f, "ck-granted", "viewer")
	globalViewer := mkUser(t, f, "ck-globalv", "viewer")

	pub := mkProject(t, f, "pub", database.VisibilityPublic)
	priv := mkProject(t, f, "priv", database.VisibilityPrivate)
	cust := mkProject(t, f, "cust", database.VisibilityCustom)

	// grantedViewer has per-project access on cust and priv. The latter
	// covers M-14: per-project grant should grant view on private too.
	if err := f.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: cust.ID, UserID: grantedViewer.ID, Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: priv.ID, UserID: grantedViewer.ID, Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	// globalViewer has a global access grant
	if err := f.global.UpsertGrant(ctx, &database.GlobalAccessGrant{
		UserID: globalViewer.ID, Role: "viewer", Source: "manual",
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		user *database.User
		proj *database.Project
		want bool
	}{
		{"anonymous + public", nil, pub, true},
		{"anonymous + private", nil, priv, false},
		{"anonymous + custom", nil, cust, false},

		{"admin + private", admin, priv, true},
		{"admin + custom", admin, cust, true},

		{"plain editor + private (no global grant)", editor, priv, false},
		{"plain editor + custom (no per-project)", editor, cust, false},

		{"plain viewer + private (no grants)", viewer, priv, false},
		{"plain viewer + custom (no grants)", viewer, cust, false},

		{"viewer w/ per-project grant + custom", grantedViewer, cust, true},
		// Per-project grant now opens private too (audit M-14). This is what
		// makes the Service.Create auto-grant rule meaningful for the default
		// `private` visibility.
		{"viewer w/ per-project grant + private", grantedViewer, priv, true},

		{"viewer w/ global grant + private", globalViewer, priv, true},
		{"viewer w/ global grant + custom (no per-project)", globalViewer, cust, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := f.checker.CanView(ctx, tc.user, tc.proj)
			if got != tc.want {
				t.Errorf("CanView = %v, want %v", got, tc.want)
			}
		})
	}
}

// CanUpload: same matrix, focused on the upload-permission rules.
func TestCanUpload(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	admin := mkUser(t, f, "up-admin", "admin")
	editor := mkUser(t, f, "up-editor", "editor")
	viewer := mkUser(t, f, "up-viewer", "viewer")
	grantedViewer := mkUser(t, f, "up-granted", "viewer")
	grantedEditor := mkUser(t, f, "up-pgranted", "viewer")
	globalEditor := mkUser(t, f, "up-globaled", "viewer")

	priv := mkProject(t, f, "uppriv", database.VisibilityPrivate)
	cust := mkProject(t, f, "upcust", database.VisibilityCustom)

	if err := f.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: cust.ID, UserID: grantedViewer.ID, Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: cust.ID, UserID: grantedEditor.ID, Role: "editor",
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.global.UpsertGrant(ctx, &database.GlobalAccessGrant{
		UserID: globalEditor.ID, Role: "editor", Source: "manual",
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		user *database.User
		proj *database.Project
		want bool
	}{
		{"anonymous", nil, cust, false},

		// Global editor/admin can upload anywhere — including a project they
		// can't VIEW. The asymmetry is the M-2 finding.
		{"admin + private", admin, priv, true},
		{"admin + custom", admin, cust, true},
		{"global editor + private", editor, priv, true},
		{"global editor + custom (no per-project)", editor, cust, true},

		{"plain viewer + private (no grants)", viewer, priv, false},
		{"plain viewer + custom (no grants)", viewer, cust, false},

		// A viewer-role per-project grant doesn't grant upload.
		{"viewer w/ per-project viewer grant + custom", grantedViewer, cust, false},
		// An editor-role per-project grant does.
		{"viewer w/ per-project editor grant + custom", grantedEditor, cust, true},

		// Global access grant with editor role permits upload on private.
		{"viewer w/ global editor grant + private", globalEditor, priv, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := f.checker.CanUpload(ctx, tc.user, tc.proj)
			if got != tc.want {
				t.Errorf("CanUpload = %v, want %v", got, tc.want)
			}
		})
	}
}

// CanManage is pure (no store lookups): admins manage everything, everyone
// else only the projects they created.
func TestCanManage(t *testing.T) {
	f := newFixture(t)

	creatorID := int64(7)
	otherID := int64(8)
	admin := &database.User{ID: 1, Role: "admin"}
	creator := &database.User{ID: creatorID, Role: "editor"}
	other := &database.User{ID: otherID, Role: "editor"}

	owned := &database.Project{Slug: "owned", Visibility: database.VisibilityCustom, CreatedBy: &creatorID}
	orphan := &database.Project{Slug: "orphan", Visibility: database.VisibilityCustom} // CreatedBy nil

	cases := []struct {
		name string
		user *database.User
		proj *database.Project
		want bool
	}{
		{"anonymous", nil, owned, false},
		{"admin manages owned", admin, owned, true},
		{"admin manages orphan (nil creator)", admin, orphan, true},
		{"creator manages own project", creator, owned, true},
		{"non-creator editor cannot manage", other, owned, false},
		{"editor cannot manage orphan", other, orphan, false},
		{"creator cannot manage someone else's", creator, &database.Project{CreatedBy: &otherID}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.checker.CanManage(tc.user, tc.proj); got != tc.want {
				t.Errorf("CanManage = %v, want %v", got, tc.want)
			}
		})
	}
}

// FilterAccessible: combines the rules from CanView in a batch form.
func TestFilterAccessible(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	editor := mkUser(t, f, "fa-editor", "editor")

	pub := mkProject(t, f, "fa-pub", database.VisibilityPublic)
	priv := mkProject(t, f, "fa-priv", database.VisibilityPrivate)
	privGranted := mkProject(t, f, "fa-priv-granted", database.VisibilityPrivate)
	custAccessible := mkProject(t, f, "fa-cust-ok", database.VisibilityCustom)
	custHidden := mkProject(t, f, "fa-cust-hidden", database.VisibilityCustom)

	if err := f.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: custAccessible.ID, UserID: editor.ID, Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	// Per-project grant on a private project (M-14: this is what the
	// Service.Create auto-grant produces for an editor's own project).
	if err := f.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: privGranted.ID, UserID: editor.ID, Role: "editor",
	}); err != nil {
		t.Fatal(err)
	}

	all := []database.Project{*pub, *priv, *privGranted, *custAccessible, *custHidden}
	got := f.checker.FilterAccessible(ctx, editor, all)

	// Editor without a global grant sees: pub (visibility) + privGranted
	// (per-project grant on private) + custAccessible (per-project grant on
	// custom). They do NOT see priv (no grants) or custHidden (no grant).
	gotSlugs := make(map[string]bool, len(got))
	for _, p := range got {
		gotSlugs[p.Slug] = true
	}
	wantSlugs := map[string]bool{"fa-pub": true, "fa-priv-granted": true, "fa-cust-ok": true}
	if len(gotSlugs) != len(wantSlugs) {
		t.Fatalf("FilterAccessible returned %d projects, want %d: %v", len(gotSlugs), len(wantSlugs), gotSlugs)
	}
	for s := range wantSlugs {
		if !gotSlugs[s] {
			t.Errorf("expected slug %q in filtered set, missing", s)
		}
	}
}

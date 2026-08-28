package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
)

// Build a fresh Service against an in-memory sqlite DB and tempdir storage.
// Returns the service plus the underlying stores so tests can verify
// post-create state.
func newServiceForTest(t *testing.T) (*Service, *sqlstore.ProjectStore, *sqlstore.ProjectAccessStore) {
	t.Helper()
	db := testutil.NewTestDB(t)
	pstore := sqlstore.NewProjectStore(db)
	vstore := sqlstore.NewVersionStore(db)
	astore := sqlstore.NewProjectAccessStore(db)
	storage := docs.NewFilesystemStorage(t.TempDir())
	svc := NewService(pstore, vstore, astore, storage, testutil.TestLogger())
	return svc, pstore, astore
}

func TestCreateInvalidSlug(t *testing.T) {
	svc, _, _ := newServiceForTest(t)
	_, err := svc.Create(context.Background(), CreateOptions{
		Slug:       "INVALID SLUG",
		Visibility: database.VisibilityCustom,
	})
	if !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("expected ErrInvalidSlug, got %v", err)
	}
}

func TestCreateInvalidVisibility(t *testing.T) {
	svc, _, _ := newServiceForTest(t)
	_, err := svc.Create(context.Background(), CreateOptions{
		Slug:       "good-slug",
		Visibility: "secret",
	})
	if !errors.Is(err, ErrInvalidVisibility) {
		t.Errorf("expected ErrInvalidVisibility, got %v", err)
	}
}

func TestCreateDefaultsName(t *testing.T) {
	svc, pstore, _ := newServiceForTest(t)
	p, err := svc.Create(context.Background(), CreateOptions{
		Slug: "name-default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "name-default" {
		t.Errorf("expected Name to default to Slug, got %q", p.Name)
	}
	// Round-trip: visibility defaults to private.
	stored, _ := pstore.GetBySlug(context.Background(), "name-default")
	if stored.Visibility != database.VisibilityPrivate {
		t.Errorf("expected default visibility=private, got %q", stored.Visibility)
	}
}

func TestCreatePublicByEditorRejected(t *testing.T) {
	svc, _, _ := newServiceForTest(t)
	editor := &database.User{ID: 1, Username: "ed", Role: "editor"}
	_, err := svc.Create(context.Background(), CreateOptions{
		Slug:       "ed-pub",
		Visibility: database.VisibilityPublic,
		Creator:    editor,
	})
	if !errors.Is(err, ErrPublicRequiresAdmin) {
		t.Errorf("expected ErrPublicRequiresAdmin, got %v", err)
	}
}

func TestCreatePublicByAdminAllowed(t *testing.T) {
	db := testutil.NewTestDB(t)
	pstore := sqlstore.NewProjectStore(db)
	vstore := sqlstore.NewVersionStore(db)
	astore := sqlstore.NewProjectAccessStore(db)
	ustore := sqlstore.NewUserStore(db)
	storage := docs.NewFilesystemStorage(t.TempDir())
	svc := NewService(pstore, vstore, astore, storage, testutil.TestLogger())

	ctx := context.Background()
	// created_by is a real FK now, so the creator must exist as a user row.
	admin := &database.User{Username: "ad", AuthSource: "builtin", Role: "admin"}
	if err := ustore.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}

	p, err := svc.Create(ctx, CreateOptions{
		Slug:       "ad-pub",
		Visibility: database.VisibilityPublic,
		Creator:    admin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Visibility != database.VisibilityPublic {
		t.Errorf("expected visibility=public, got %q", p.Visibility)
	}
	if p.CreatedBy == nil || *p.CreatedBy != admin.ID {
		t.Errorf("expected CreatedBy=%d, got %v", admin.ID, p.CreatedBy)
	}
	// Round-trip: created_by persists.
	stored, _ := pstore.GetBySlug(ctx, "ad-pub")
	if stored.CreatedBy == nil || *stored.CreatedBy != admin.ID {
		t.Errorf("expected stored CreatedBy=%d, got %v", admin.ID, stored.CreatedBy)
	}
}

// Granting an auto-editor on a custom project requires a real user row
// (FK from project_access.user_id → users.id). The newServiceForTest helper
// returns just the access store; we build the full set here so we can also
// create the user.
func TestCreateGrantsCreatorEditorAccess(t *testing.T) {
	db := testutil.NewTestDB(t)
	pstore := sqlstore.NewProjectStore(db)
	vstore := sqlstore.NewVersionStore(db)
	astore := sqlstore.NewProjectAccessStore(db)
	ustore := sqlstore.NewUserStore(db)
	storage := docs.NewFilesystemStorage(t.TempDir())
	svc := NewService(pstore, vstore, astore, storage, testutil.TestLogger())

	ctx := context.Background()
	editor := &database.User{Username: "creator", AuthSource: "builtin", Role: "editor"}
	if err := ustore.Create(ctx, editor); err != nil {
		t.Fatal(err)
	}

	p, err := svc.Create(ctx, CreateOptions{
		Slug:       "custom-grant",
		Visibility: database.VisibilityCustom,
		Creator:    editor,
	})
	if err != nil {
		t.Fatal(err)
	}

	role, err := astore.GetEffectiveRole(ctx, p.ID, editor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if role != "editor" {
		t.Errorf("expected creator to be auto-granted editor role, got %q", role)
	}
}

// Admins do NOT get an auto-grant (they don't need one; their global role
// allows access). The project_access table should be empty for the project.
func TestCreateAdminGetsNoAutoGrant(t *testing.T) {
	db := testutil.NewTestDB(t)
	pstore := sqlstore.NewProjectStore(db)
	vstore := sqlstore.NewVersionStore(db)
	astore := sqlstore.NewProjectAccessStore(db)
	ustore := sqlstore.NewUserStore(db)
	storage := docs.NewFilesystemStorage(t.TempDir())
	svc := NewService(pstore, vstore, astore, storage, testutil.TestLogger())

	ctx := context.Background()
	admin := &database.User{Username: "ad-creator", AuthSource: "builtin", Role: "admin"}
	if err := ustore.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}

	p, err := svc.Create(ctx, CreateOptions{
		Slug:       "admin-no-grant",
		Visibility: database.VisibilityCustom,
		Creator:    admin,
	})
	if err != nil {
		t.Fatal(err)
	}
	grants, err := astore.ListByProject(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Errorf("expected no project_access rows for admin-created project, got %d", len(grants))
	}
}

// buildRenameFixture creates a project with one deployed version (DB row +
// on-disk files) and returns everything a rename test needs to verify state.
func buildRenameFixture(t *testing.T, slug string) (*Service, *sqlstore.ProjectStore, *sqlstore.VersionStore, *docs.FilesystemStorage, *database.Project) {
	t.Helper()
	db := testutil.NewTestDB(t)
	pstore := sqlstore.NewProjectStore(db)
	vstore := sqlstore.NewVersionStore(db)
	astore := sqlstore.NewProjectAccessStore(db)
	storage := docs.NewFilesystemStorage(t.TempDir())
	svc := NewService(pstore, vstore, astore, storage, testutil.TestLogger())

	ctx := context.Background()
	p, err := svc.Create(ctx, CreateOptions{Slug: slug, Visibility: database.VisibilityCustom})
	if err != nil {
		t.Fatal(err)
	}

	if err := storage.EnsureVersionDir(slug, "v1.0"); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(storage.VersionPath(slug, "v1.0"), "index.html")
	if err := os.WriteFile(marker, []byte("<html>doc</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	ver := &database.Version{
		ProjectID:   p.ID,
		Tag:         "v1.0",
		StoragePath: storage.VersionPath(slug, "v1.0"),
		ContentType: "archive",
	}
	if err := vstore.Create(ctx, ver); err != nil {
		t.Fatal(err)
	}
	return svc, pstore, vstore, storage, p
}

func TestUpdateRenameMigratesStorage(t *testing.T) {
	svc, pstore, vstore, storage, p := buildRenameFixture(t, "old-name")
	ctx := context.Background()

	p.Slug = "new-name"
	if err := svc.Update(ctx, p, "old-name"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// DB row carries the new slug.
	stored, _ := pstore.GetBySlug(ctx, "new-name")
	if stored == nil {
		t.Fatal("project not found under new slug")
	}

	// Files moved to the new path; old path is gone.
	if _, err := os.Stat(storage.ProjectPath("old-name")); !os.IsNotExist(err) {
		t.Errorf("old storage dir should be gone, err = %v", err)
	}
	moved := filepath.Join(storage.VersionPath("new-name", "v1.0"), "index.html")
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("doc should have moved to new path: %v", err)
	}

	// Version StoragePath rewritten so reindex finds the files.
	versions, _ := vstore.ListByProject(ctx, p.ID)
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	want := storage.VersionPath("new-name", "v1.0")
	if versions[0].StoragePath != want {
		t.Errorf("version StoragePath = %q, want %q", versions[0].StoragePath, want)
	}
}

func TestUpdateRenameRollsBackOnStorageFailure(t *testing.T) {
	svc, pstore, _, storage, p := buildRenameFixture(t, "orig")
	ctx := context.Background()

	// Pre-create an orphan directory at the destination so MoveProject refuses
	// to overwrite it — the DB has no project there, so the row update itself
	// succeeds and we exercise the rollback path.
	if err := storage.EnsureProjectDir("blocked"); err != nil {
		t.Fatal(err)
	}

	p.Slug = "blocked"
	if err := svc.Update(ctx, p, "orig"); err == nil {
		t.Fatal("expected error when storage move fails")
	}

	// Slug must be rolled back in the DB so the project stays reachable.
	if stored, _ := pstore.GetBySlug(ctx, "orig"); stored == nil {
		t.Error("project should remain reachable under its original slug")
	}
	if p.Slug != "orig" {
		t.Errorf("in-memory slug should be rolled back to orig, got %q", p.Slug)
	}
	// Original files untouched.
	if _, err := os.Stat(storage.VersionPath("orig", "v1.0")); err != nil {
		t.Errorf("original files should be intact: %v", err)
	}
}

func TestUpdateRenameSlugConflict(t *testing.T) {
	svc, _, _, _, p := buildRenameFixture(t, "keep")
	ctx := context.Background()

	// A second project occupies the target slug.
	if _, err := svc.Create(ctx, CreateOptions{Slug: "taken", Visibility: database.VisibilityCustom}); err != nil {
		t.Fatal(err)
	}

	p.Slug = "taken"
	if err := svc.Update(ctx, p, "keep"); !errors.Is(err, ErrSlugConflict) {
		t.Errorf("expected ErrSlugConflict, got %v", err)
	}
}

func TestCreateSlugConflict(t *testing.T) {
	svc, _, _ := newServiceForTest(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, CreateOptions{Slug: "dup", Visibility: database.VisibilityCustom})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Create(ctx, CreateOptions{Slug: "dup", Visibility: database.VisibilityCustom})
	if !errors.Is(err, ErrSlugConflict) {
		t.Errorf("expected ErrSlugConflict on duplicate slug, got %v", err)
	}
}


// A project with deployed versions whose storage directory has gone missing
// must not have its rename committed: doing so leaves the docs unreachable at
// both the old and the new slug (issue #129).
func TestUpdateRenameRefusedWhenStorageMissing(t *testing.T) {
	svc, pstore, vstore, storage, p := buildRenameFixture(t, "divorced")
	ctx := context.Background()

	// Move the directory out from under the app, as a rename predating the
	// issue #122 fix would have left it.
	base := storage.BasePath()
	if err := os.Rename(filepath.Join(base, "divorced"), filepath.Join(base, "stale")); err != nil {
		t.Fatal(err)
	}

	p.Slug = "renamed"
	err := svc.Update(ctx, p, "divorced")
	if !errors.Is(err, ErrStorageMissing) {
		t.Fatalf("Update err = %v, want ErrStorageMissing", err)
	}

	// The rename must have been rolled back, not committed.
	if stored, _ := pstore.GetBySlug(ctx, "renamed"); stored != nil {
		t.Error("rename should not have been committed under the new slug")
	}
	if stored, _ := pstore.GetBySlug(ctx, "divorced"); stored == nil {
		t.Error("project should still resolve under its original slug")
	}
	if p.Slug != "divorced" {
		t.Errorf("in-memory slug = %q, want rollback to divorced", p.Slug)
	}

	// Version rows untouched, so ReconcileStorage can still find the files.
	versions, _ := vstore.ListByProject(ctx, p.ID)
	if len(versions) != 1 || versions[0].StoragePath != storage.VersionPath("divorced", "v1.0") {
		t.Error("version StoragePath should be left as the recovery breadcrumb")
	}
}

// A project that was never deployed has no directory to move, so renaming it
// must still succeed.
func TestUpdateRenameNeverDeployedSucceeds(t *testing.T) {
	db := testutil.NewTestDB(t)
	pstore := sqlstore.NewProjectStore(db)
	storage := docs.NewFilesystemStorage(t.TempDir())
	svc := NewService(pstore, sqlstore.NewVersionStore(db), sqlstore.NewProjectAccessStore(db), storage, testutil.TestLogger())
	ctx := context.Background()

	p, err := svc.Create(ctx, CreateOptions{Slug: "empty", Visibility: database.VisibilityCustom})
	if err != nil {
		t.Fatal(err)
	}
	// Create() eagerly makes the project dir; drop it so there is nothing at all.
	if err := os.RemoveAll(storage.ProjectPath("empty")); err != nil {
		t.Fatal(err)
	}

	p.Slug = "still-empty"
	if err := svc.Update(ctx, p, "empty"); err != nil {
		t.Fatalf("renaming a never-deployed project should succeed, got %v", err)
	}
	if stored, _ := pstore.GetBySlug(ctx, "still-empty"); stored == nil {
		t.Error("project should have been renamed")
	}
}

// ReconcileStorage moves a divorced project's files back under its slug and
// rewrites the version paths.
func TestReconcileStorageRepairsDivorcedProject(t *testing.T) {
	svc, _, vstore, storage, p := buildRenameFixture(t, "realdir")
	ctx := context.Background()

	// Simulate the pre-#122 state: DB slug changed, files left behind. The
	// version StoragePath still points at the real location.
	p.Slug = "newslug"
	if err := svc.projects.Update(ctx, p); err != nil {
		t.Fatal(err)
	}

	repaired, err := svc.ReconcileStorage(ctx)
	if err != nil {
		t.Fatalf("ReconcileStorage: %v", err)
	}
	if repaired != 1 {
		t.Fatalf("repaired = %d, want 1", repaired)
	}

	// Files now live under the current slug.
	doc := filepath.Join(storage.VersionPath("newslug", "v1.0"), "index.html")
	if _, err := os.Stat(doc); err != nil {
		t.Errorf("docs should be reachable under the current slug: %v", err)
	}
	if _, err := os.Stat(storage.ProjectPath("realdir")); !os.IsNotExist(err) {
		t.Error("stale directory should be gone")
	}

	versions, _ := vstore.ListByProject(ctx, p.ID)
	if want := storage.VersionPath("newslug", "v1.0"); versions[0].StoragePath != want {
		t.Errorf("StoragePath = %q, want %q", versions[0].StoragePath, want)
	}
}

// Healthy installations must come through reconciliation untouched.
func TestReconcileStorageLeavesHealthyProjectsAlone(t *testing.T) {
	svc, _, vstore, storage, p := buildRenameFixture(t, "healthy")
	ctx := context.Background()

	repaired, err := svc.ReconcileStorage(ctx)
	if err != nil {
		t.Fatalf("ReconcileStorage: %v", err)
	}
	if repaired != 0 {
		t.Errorf("repaired = %d, want 0 on a healthy install", repaired)
	}
	if _, err := os.Stat(storage.VersionPath("healthy", "v1.0")); err != nil {
		t.Errorf("files should be untouched: %v", err)
	}
	versions, _ := vstore.ListByProject(ctx, p.ID)
	if want := storage.VersionPath("healthy", "v1.0"); versions[0].StoragePath != want {
		t.Errorf("StoragePath = %q, want %q", versions[0].StoragePath, want)
	}
}

// A breadcrumb pointing outside the storage root must never be followed.
func TestReconcileStorageIgnoresPathOutsideRoot(t *testing.T) {
	svc, _, vstore, storage, p := buildRenameFixture(t, "escape")
	ctx := context.Background()

	outside := t.TempDir()
	versions, _ := vstore.ListByProject(ctx, p.ID)
	versions[0].StoragePath = filepath.Join(outside, "elsewhere", "v1.0")
	if err := vstore.Update(ctx, &versions[0]); err != nil {
		t.Fatal(err)
	}
	// Remove the real dir so the project looks divorced.
	if err := os.RemoveAll(storage.ProjectPath("escape")); err != nil {
		t.Fatal(err)
	}

	repaired, err := svc.ReconcileStorage(ctx)
	if err != nil {
		t.Fatalf("ReconcileStorage: %v", err)
	}
	if repaired != 0 {
		t.Errorf("repaired = %d, want 0 for a path outside the storage root", repaired)
	}
}

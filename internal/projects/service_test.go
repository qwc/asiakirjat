package projects

import (
	"context"
	"errors"
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
	astore := sqlstore.NewProjectAccessStore(db)
	storage := docs.NewFilesystemStorage(t.TempDir())
	svc := NewService(pstore, astore, storage, testutil.TestLogger())
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
	svc, _, _ := newServiceForTest(t)
	admin := &database.User{ID: 1, Username: "ad", Role: "admin"}
	p, err := svc.Create(context.Background(), CreateOptions{
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
}

// Granting an auto-editor on a custom project requires a real user row
// (FK from project_access.user_id → users.id). The newServiceForTest helper
// returns just the access store; we build the full set here so we can also
// create the user.
func TestCreateGrantsCreatorEditorAccess(t *testing.T) {
	db := testutil.NewTestDB(t)
	pstore := sqlstore.NewProjectStore(db)
	astore := sqlstore.NewProjectAccessStore(db)
	ustore := sqlstore.NewUserStore(db)
	storage := docs.NewFilesystemStorage(t.TempDir())
	svc := NewService(pstore, astore, storage, testutil.TestLogger())

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
	astore := sqlstore.NewProjectAccessStore(db)
	ustore := sqlstore.NewUserStore(db)
	storage := docs.NewFilesystemStorage(t.TempDir())
	svc := NewService(pstore, astore, storage, testutil.TestLogger())

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


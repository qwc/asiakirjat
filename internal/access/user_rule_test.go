package access

import (
	"context"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
)

// TestUserRuleGrantsPrivateAccess pins what Admin > Global Access promises:
// naming a user there (subject_type 'user') lets them reach private-visibility
// projects. The same rules arrive from config via access.private.viewers.users.
//
// Only ldap_group and oauth2_group rules were ever resolved into grants, by the
// LDAP/OAuth2 login sync. User rules resolved nowhere — main.go's
// syncGlobalAccessConfig had a loop over them whose body was `continue` — so
// adding a user by name silently did nothing.
func TestUserRuleGrantsPrivateAccess(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	named := mkUser(t, f, "named-viewer", "viewer")
	other := mkUser(t, f, "unnamed-viewer", "viewer")
	priv := mkProject(t, f, "priv-user-rule", database.VisibilityPrivate)

	if err := f.global.CreateRule(ctx, &database.GlobalAccess{
		SubjectType:       "user",
		SubjectIdentifier: named.Username,
		Role:              "viewer",
	}); err != nil {
		t.Fatal(err)
	}

	if !f.checker.CanView(ctx, named, priv) {
		t.Error("a user named in a global access rule should be able to view private projects")
	}
	if f.checker.CanView(ctx, other, priv) {
		t.Error("a user with no rule and no grant must not reach private projects")
	}
}

// TestUserRuleEditorCanUpload covers the editor half of the same promise.
func TestUserRuleEditorCanUpload(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	named := mkUser(t, f, "named-editor", "viewer") // global role stays viewer
	priv := mkProject(t, f, "priv-editor-rule", database.VisibilityPrivate)

	if err := f.global.CreateRule(ctx, &database.GlobalAccess{
		SubjectType:       "user",
		SubjectIdentifier: named.Username,
		Role:              "editor",
	}); err != nil {
		t.Fatal(err)
	}

	if !f.checker.CanUpload(ctx, named, priv) {
		t.Error("a user named as editor in a global access rule should be able to upload")
	}
}

package database

import (
	"testing"
)

// TestMigrationNormalizesAdminRoles covers the data half of audit L-1. The
// read paths no longer treat 'admin' as a grant role, so any row that predates
// that has to be normalised — otherwise dropping those branches would revoke
// access from an existing deployment rather than being a no-op.
func TestMigrationNormalizesAdminRoles(t *testing.T) {
	db, _, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := RunMigrations(db, DialectSQLite); err != nil {
		t.Fatal(err)
	}

	db.MustExec(`INSERT INTO projects (slug, name, visibility) VALUES ('p', 'P', 'custom')`)
	db.MustExec(`INSERT INTO users (username, auth_source, role) VALUES ('u', 'builtin', 'viewer')`)

	// Roles written before the stores constrained them. Inserted directly:
	// the stores now refuse these.
	db.MustExec(`INSERT INTO project_access (project_id, user_id, role, source) VALUES (1, 1, 'admin', 'manual')`)
	db.MustExec(`INSERT INTO global_access (subject_type, subject_identifier, role) VALUES ('user', 'u', 'admin')`)
	db.MustExec(`INSERT INTO global_access_grants (user_id, role, source) VALUES (1, 'admin', 'ldap')`)
	db.MustExec(`INSERT INTO auth_group_mappings (auth_source, group_identifier, project_id, role) VALUES ('ldap', 'cn=eng', 1, 'admin')`)

	m := migrator(t, db)
	if err := m.Migrate(14); err != nil {
		t.Fatalf("migrating down to 014: %v", err)
	}
	if err := m.Migrate(15); err != nil {
		t.Fatalf("migrating back up to 015: %v", err)
	}

	for _, table := range []string{"project_access", "global_access", "global_access_grants", "auth_group_mappings"} {
		var admins int
		if err := db.Get(&admins, `SELECT COUNT(*) FROM `+table+` WHERE role = 'admin'`); err != nil {
			t.Fatal(err)
		}
		if admins != 0 {
			t.Errorf("expected no admin roles left in %s, got %d", table, admins)
		}

		var editors int
		if err := db.Get(&editors, `SELECT COUNT(*) FROM `+table+` WHERE role = 'editor'`); err != nil {
			t.Fatal(err)
		}
		if editors != 1 {
			t.Errorf("expected the admin row in %s to become an editor, got %d editors", table, editors)
		}
	}
}

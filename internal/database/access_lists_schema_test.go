package database

import (
	"testing"
)

// TestAccessListMigrationRoundTrip exercises migration 012 in both
// directions. Going down has to unwind a project that points at a list: the
// pointer is dropped and the project falls back to 'custom' so it fails
// closed rather than widening to a broader rule.
func TestAccessListMigrationRoundTrip(t *testing.T) {
	db, _, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := RunMigrations(db, DialectSQLite); err != nil {
		t.Fatal(err)
	}

	db.MustExec(`INSERT INTO access_lists (name, description) VALUES ('engineering', 'Dev team')`)
	db.MustExec(`INSERT INTO access_list_members (list_id, subject_type, subject_identifier, role)
		VALUES (1, 'ldap_group', 'cn=eng,dc=example,dc=com', 'editor'), (1, 'user', 'alice', 'viewer')`)
	db.MustExec(`INSERT INTO projects (slug, name, visibility, access_list_id)
		VALUES ('guarded', 'Guarded', 'list', 1)`)

	m := migrator(t, db)
	if err := m.Migrate(11); err != nil {
		t.Fatalf("migrating down to 011: %v", err)
	}

	var visibility string
	if err := db.Get(&visibility, `SELECT visibility FROM projects WHERE slug = 'guarded'`); err != nil {
		t.Fatal(err)
	}
	if visibility != VisibilityCustom {
		t.Errorf("expected a list-governed project to fall back to custom, got %q", visibility)
	}

	if err := m.Migrate(12); err != nil {
		t.Fatalf("migrating back up to 012: %v", err)
	}

	var tables int
	if err := db.Get(&tables, `SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name IN ('access_lists', 'access_list_members')`); err != nil {
		t.Fatal(err)
	}
	if tables != 2 {
		t.Errorf("expected both access list tables after the round trip, got %d", tables)
	}
}

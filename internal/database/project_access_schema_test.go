package database

import (
	"sort"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	sqlitemigrate "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
)

// indexColumns returns, for every unique index on the given SQLite table,
// the sorted set of columns it covers keyed by index name.
func uniqueIndexColumns(t *testing.T, db interface {
	Select(dest any, query string, args ...any) error
}, table string) map[string][]string {
	t.Helper()

	type indexRow struct {
		Seq     int    `db:"seq"`
		Name    string `db:"name"`
		Unique  bool   `db:"unique"`
		Origin  string `db:"origin"`
		Partial bool   `db:"partial"`
	}
	var indexes []indexRow
	if err := db.Select(&indexes, "PRAGMA index_list("+table+")"); err != nil {
		t.Fatalf("index_list(%s): %v", table, err)
	}

	type infoRow struct {
		Seqno int     `db:"seqno"`
		Cid   int     `db:"cid"`
		Name  *string `db:"name"`
	}
	out := make(map[string][]string)
	for _, idx := range indexes {
		if !idx.Unique {
			continue
		}
		var info []infoRow
		if err := db.Select(&info, "PRAGMA index_info("+idx.Name+")"); err != nil {
			t.Fatalf("index_info(%s): %v", idx.Name, err)
		}
		var cols []string
		for _, c := range info {
			if c.Name != nil {
				cols = append(cols, *c.Name)
			}
		}
		sort.Strings(cols)
		out[idx.Name] = cols
	}
	return out
}

// TestProjectAccessUniqueOnSource pins the schema that migration 002 set out
// to produce: uniqueness on project_access is per (project_id, user_id,
// source), so one user can hold grants from several sources on the same
// project. The original UNIQUE(project_id, user_id) from 001 must be gone.
//
// On SQLite it was not: 002's `DROP INDEX IF EXISTS idx_project_access_unique`
// never matched the auto-index behind the table-level constraint, so both
// were live and any second-source Grant failed (issue #133).
func TestProjectAccessUniqueOnSource(t *testing.T) {
	db, _, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := RunMigrations(db, DialectSQLite); err != nil {
		t.Fatal(err)
	}

	indexes := uniqueIndexColumns(t, db, "project_access")

	wantWithSource := []string{"project_id", "source", "user_id"} // sorted
	foundWithSource := false
	for name, cols := range indexes {
		joined := strings.Join(cols, ",")
		if joined == strings.Join(wantWithSource, ",") {
			foundWithSource = true
			continue
		}
		if joined == "project_id,user_id" {
			t.Errorf("unique index %q still constrains (project_id, user_id) without source; "+
				"a user cannot then hold grants from two sources on one project", name)
		}
	}
	if !foundWithSource {
		t.Errorf("expected a unique index over (project_id, user_id, source), got %v", indexes)
	}
}

// migrator builds a migrate.Migrate over the embedded SQLite migrations so a
// test can step versions in both directions; RunMigrations only goes up.
func migrator(t *testing.T, db *sqlx.DB) *migrate.Migrate {
	t.Helper()

	source, err := iofs.New(sqliteMigrations, "migrations/sqlite")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := sqlitemigrate.WithInstance(db.DB, &sqlitemigrate.Config{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestProjectAccessSourceUniqueRoundTrip exercises the down migration, which
// has to collapse multi-source grants back into one row per user before the
// old UNIQUE(project_id, user_id) constraint can hold. It keeps the manual
// grant when there is one.
func TestProjectAccessSourceUniqueRoundTrip(t *testing.T) {
	db, _, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := RunMigrations(db, DialectSQLite); err != nil {
		t.Fatal(err)
	}

	db.MustExec(`INSERT INTO projects (slug, name, visibility) VALUES ('p', 'P', 'custom')`)
	db.MustExec(`INSERT INTO users (username, auth_source, role) VALUES ('u', 'ldap', 'viewer')`)
	db.MustExec(`INSERT INTO project_access (project_id, user_id, role, source)
		VALUES (1, 1, 'viewer', 'ldap'), (1, 1, 'editor', 'manual')`)

	m := migrator(t, db)
	if err := m.Migrate(10); err != nil {
		t.Fatalf("migrating down to 010: %v", err)
	}

	var rows []ProjectAccess
	if err := db.Select(&rows, `SELECT * FROM project_access`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected the down migration to collapse to one row, got %d: %+v", len(rows), rows)
	}
	if rows[0].Source != "manual" {
		t.Errorf("expected the manual grant to be kept, got %q", rows[0].Source)
	}

	if err := m.Migrate(11); err != nil {
		t.Fatalf("migrating back up to 011: %v", err)
	}

	indexes := uniqueIndexColumns(t, db, "project_access")
	for name, cols := range indexes {
		if strings.Join(cols, ",") == "project_id,user_id" {
			t.Errorf("unique index %q reappeared after the round trip", name)
		}
	}
}

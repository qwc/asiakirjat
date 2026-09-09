package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDetectDialect(t *testing.T) {
	tests := []struct {
		driver string
		want   Dialect
	}{
		{"sqlite", DialectSQLite},
		{"sqlite3", DialectSQLite},
		{"SQLITE", DialectSQLite},
		{"postgres", DialectPostgres},
		{"pgx", DialectPostgres},
		{"postgresql", DialectPostgres},
		{"mysql", DialectMySQL},
		{"mariadb", DialectMySQL},
		{"unknown", DialectSQLite},
		{"", DialectSQLite},
	}

	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			got := DetectDialect(tt.driver)
			if got != tt.want {
				t.Errorf("DetectDialect(%q) = %v, want %v", tt.driver, got, tt.want)
			}
		})
	}
}

func TestOpenSQLite(t *testing.T) {
	db, dialect, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if dialect != DialectSQLite {
		t.Errorf("expected sqlite dialect, got %v", dialect)
	}

	// Verify it works
	var result int
	if err := db.Get(&result, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestMigrations(t *testing.T) {
	db, _, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := RunMigrations(db, DialectSQLite); err != nil {
		t.Fatal(err)
	}

	// Verify tables exist
	tables := []string{"users", "sessions", "projects", "versions", "project_access", "api_tokens"}
	for _, table := range tables {
		var count int
		query := "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
		if err := db.Get(&count, query, table); err != nil {
			t.Fatalf("checking table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("table %s not found", table)
		}
	}
}

func TestSQLiteDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "bare path gets both pragmas",
			dsn:  "data/asiakirjat.db",
			want: "data/asiakirjat.db?_pragma=foreign_keys%281%29&_pragma=journal_mode%28WAL%29",
		},
		{
			name: "existing query is preserved",
			dsn:  "file:data/asiakirjat.db?_txlock=immediate",
			want: "file:data/asiakirjat.db?_pragma=foreign_keys%281%29&_pragma=journal_mode%28WAL%29&_txlock=immediate",
		},
		{
			name: "operator's own journal_mode is left alone",
			dsn:  "data/asiakirjat.db?_pragma=journal_mode(DELETE)",
			want: "data/asiakirjat.db?_pragma=journal_mode%28DELETE%29&_pragma=foreign_keys%281%29",
		},
		{
			name: "unparseable query is handed to the driver untouched",
			dsn:  "data/asiakirjat.db?%zz",
			want: "data/asiakirjat.db?%zz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sqliteDSN(tt.dsn); got != tt.want {
				t.Errorf("sqliteDSN(%q)\n got %q\nwant %q", tt.dsn, got, tt.want)
			}
		})
	}
}

// TestSQLiteCascadesOnEveryConnection is the regression test for the orphan
// rows found on a production database: `PRAGMA foreign_keys=ON` was executed
// against the pool, so it reached one connection and left the rest with
// foreign keys off. Deletes served by those connections skipped their
// ON DELETE CASCADE and silently left children behind.
//
// It asserts the cascade actually fires rather than reading the pragma back,
// and it does so on several connections held open at once so the pool is
// forced to create more than one.
func TestSQLiteCascadesOnEveryConnection(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "cascade.db")

	db, _, err := Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.MustExec(`CREATE TABLE parents (id INTEGER PRIMARY KEY)`)
	db.MustExec(`CREATE TABLE children (
		id INTEGER PRIMARY KEY,
		parent_id INTEGER NOT NULL REFERENCES parents(id) ON DELETE CASCADE
	)`)

	ctx := context.Background()
	const connections = 4
	db.SetMaxOpenConns(connections)

	// Held open together, so each iteration is served by a connection the pool
	// had to open separately.
	var held []*sql.Conn
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()

	for i := 1; i <= connections; i++ {
		conn, err := db.Connx(ctx)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		held = append(held, conn.Conn)

		if _, err := conn.ExecContext(ctx, `INSERT INTO parents (id) VALUES (?)`, i); err != nil {
			t.Fatalf("connection %d: inserting parent: %v", i, err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO children (id, parent_id) VALUES (?, ?)`, i, i); err != nil {
			t.Fatalf("connection %d: inserting child: %v", i, err)
		}
		if _, err := conn.ExecContext(ctx, `DELETE FROM parents WHERE id = ?`, i); err != nil {
			t.Fatalf("connection %d: deleting parent: %v", i, err)
		}

		var orphans int
		if err := conn.GetContext(ctx, &orphans, `SELECT COUNT(*) FROM children WHERE parent_id = ?`, i); err != nil {
			t.Fatalf("connection %d: counting orphans: %v", i, err)
		}
		if orphans != 0 {
			t.Errorf("connection %d: cascade did not fire, %d orphaned child rows left behind", i, orphans)
		}
	}
}

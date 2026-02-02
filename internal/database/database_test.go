package database

import (
	"testing"
)

func TestDetectDialect(t *testing.T) {
	tests := []struct {
		driver  string
		want    Dialect
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

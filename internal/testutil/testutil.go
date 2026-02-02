package testutil

import (
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
	_ "modernc.org/sqlite"
)

func NewTestDB(t *testing.T) *sqlx.DB {
	t.Helper()

	db, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}

	db.MustExec("PRAGMA foreign_keys=ON")

	if err := database.RunMigrations(db, database.DialectSQLite); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

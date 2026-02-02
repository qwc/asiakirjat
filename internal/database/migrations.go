package database

import (
	"embed"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

func RunMigrations(db *sqlx.DB, dialect Dialect) error {
	slog.Info("running migrations", "dialect", dialect)

	var fs embed.FS
	var subdir string

	switch dialect {
	case DialectSQLite:
		fs = sqliteMigrations
		subdir = "migrations/sqlite"
	default:
		return fmt.Errorf("migrations not yet supported for dialect %s", dialect)
	}

	source, err := iofs.New(fs, subdir)
	if err != nil {
		return fmt.Errorf("creating migration source: %w", err)
	}

	var m *migrate.Migrate

	switch dialect {
	case DialectSQLite:
		driver, err := sqlite.WithInstance(db.DB, &sqlite.Config{})
		if err != nil {
			return fmt.Errorf("creating sqlite migration driver: %w", err)
		}
		m, err = migrate.NewWithInstance("iofs", source, "sqlite", driver)
		if err != nil {
			return fmt.Errorf("creating migrate instance: %w", err)
		}
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("running migrations: %w", err)
	}

	slog.Info("migrations complete")
	return nil
}

package database

import (
	"embed"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

//go:embed migrations/mysql/*.sql
var mysqlMigrations embed.FS

func RunMigrations(db *sqlx.DB, dialect Dialect) error {
	slog.Info("running migrations", "dialect", dialect)

	var embeddedFS embed.FS
	var subdir string

	switch dialect {
	case DialectSQLite:
		embeddedFS = sqliteMigrations
		subdir = "migrations/sqlite"
	case DialectPostgres:
		embeddedFS = postgresMigrations
		subdir = "migrations/postgres"
	case DialectMySQL:
		embeddedFS = mysqlMigrations
		subdir = "migrations/mysql"
	default:
		return fmt.Errorf("migrations not supported for dialect %s", dialect)
	}

	source, err := iofs.New(embeddedFS, subdir)
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
	case DialectPostgres:
		driver, err := migratepostgres.WithInstance(db.DB, &migratepostgres.Config{})
		if err != nil {
			return fmt.Errorf("creating postgres migration driver: %w", err)
		}
		m, err = migrate.NewWithInstance("iofs", source, "postgres", driver)
		if err != nil {
			return fmt.Errorf("creating migrate instance: %w", err)
		}
	case DialectMySQL:
		driver, err := migratemysql.WithInstance(db.DB, &migratemysql.Config{})
		if err != nil {
			return fmt.Errorf("creating mysql migration driver: %w", err)
		}
		m, err = migrate.NewWithInstance("iofs", source, "mysql", driver)
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

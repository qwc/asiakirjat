package database

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectPostgres Dialect = "postgres"
	DialectMySQL    Dialect = "mysql"
)

func DetectDialect(driver string) Dialect {
	switch strings.ToLower(driver) {
	case "postgres", "pgx", "postgresql":
		return DialectPostgres
	case "mysql", "mariadb":
		return DialectMySQL
	default:
		return DialectSQLite
	}
}

func Open(driver, dsn string) (*sqlx.DB, Dialect, error) {
	dialect := DetectDialect(driver)

	driverName := string(dialect)
	switch dialect {
	case DialectSQLite:
		driverName = "sqlite"
	case DialectPostgres:
		driverName = "pgx"
	case DialectMySQL:
		driverName = "mysql"
	}

	slog.Info("opening database", "driver", driverName, "dialect", dialect)

	db, err := sqlx.Open(driverName, dsn)
	if err != nil {
		return nil, "", fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, "", fmt.Errorf("pinging database: %w", err)
	}

	// Enable WAL mode and foreign keys for SQLite
	if dialect == DialectSQLite {
		db.MustExec("PRAGMA journal_mode=WAL")
		db.MustExec("PRAGMA foreign_keys=ON")
	}

	return db, dialect, nil
}

package database

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
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

	var driverName string
	switch dialect {
	case DialectSQLite:
		driverName = "sqlite"
	case DialectPostgres:
		driverName = "pgx"
	case DialectMySQL:
		driverName = "mysql"
	default:
		driverName = "sqlite"
	}

	slog.Info("opening database", "driver", driverName, "dialect", dialect)

	if dialect == DialectSQLite {
		dsn = sqliteDSN(dsn)
	}

	db, err := sqlx.Open(driverName, dsn)
	if err != nil {
		return nil, "", fmt.Errorf("opening database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, "", fmt.Errorf("pinging database: %w", err)
	}

	if dialect == DialectSQLite {
		if err := verifyForeignKeys(db); err != nil {
			db.Close()
			return nil, "", err
		}
	}

	return db, dialect, nil
}

// sqliteDSN puts the pragmas this application depends on into the DSN, so the
// driver applies them every time it opens a connection.
//
// They used to be executed as `db.MustExec("PRAGMA foreign_keys=ON")` after
// Open. That is a pool, not a connection: the pragma reached whichever
// connection happened to serve it and every other connection kept SQLite's
// default of foreign keys OFF. Deleting a project on one of those connections
// skipped ON DELETE CASCADE entirely, leaving its versions, upload logs,
// access rows and API tokens behind as orphans -- which then broke the next
// migration that had to rebuild a table, because the rebuild does enforce the
// constraints. A pragma that only sometimes applies is worse than one that
// never does; this makes it always.
//
// journal_mode is a property of the database file rather than the connection,
// so repeating it per connection is a no-op after the first. It travels here
// anyway to keep both settings in one place.
func sqliteDSN(dsn string) string {
	base, query, _ := strings.Cut(dsn, "?")

	values, err := url.ParseQuery(query)
	if err != nil {
		// Malformed query strings are the operator's to fix, and the driver
		// reports them precisely. Appending to one would only bury that.
		return dsn
	}

	for _, pragma := range []string{"foreign_keys(1)", "journal_mode(WAL)"} {
		name, _, _ := strings.Cut(pragma, "(")
		if hasPragma(values["_pragma"], name) {
			continue
		}
		values.Add("_pragma", pragma)
	}

	return base + "?" + values.Encode()
}

// hasPragma reports whether the DSN already sets the named pragma, so an
// operator who tuned one keeps their value instead of silently getting ours.
func hasPragma(pragmas []string, name string) bool {
	for _, p := range pragmas {
		set, _, _ := strings.Cut(strings.TrimSpace(p), "(")
		if strings.EqualFold(strings.TrimSpace(set), name) {
			return true
		}
	}
	return false
}

// verifyForeignKeys fails startup if foreign keys are not actually enforced.
//
// The bug this guards against was invisible: enforcement was silently off for
// most connections and the damage only surfaced months later, as orphan rows
// that broke a migration. Asserting it here turns a silent data-integrity
// failure into a loud startup failure.
func verifyForeignKeys(db *sqlx.DB) error {
	var on bool
	if err := db.Get(&on, "PRAGMA foreign_keys"); err != nil {
		return fmt.Errorf("checking foreign_keys pragma: %w", err)
	}
	if !on {
		return fmt.Errorf("foreign keys are not enabled on this connection; " +
			"remove any _pragma=foreign_keys setting from the database DSN")
	}
	return nil
}

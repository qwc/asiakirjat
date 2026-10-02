package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsInsecureInitialAdminPassword(t *testing.T) {
	cases := []struct {
		pw       string
		insecure bool
	}{
		// Banned defaults — regardless of length.
		{"changeme", true},
		{"admin", true},
		{"password", true},

		// Too short.
		{"", true},
		{"short", true},
		{strings.Repeat("a", minInitialAdminPasswordLen-1), true},

		// Long enough and not a banned default.
		{strings.Repeat("a", minInitialAdminPasswordLen), false},
		{"correct-horse-battery-staple", false},
		{"a-Reasonable-Pass-123", false},
	}

	for _, tc := range cases {
		t.Run(tc.pw, func(t *testing.T) {
			if got := isInsecureInitialAdminPassword(tc.pw); got != tc.insecure {
				t.Errorf("isInsecureInitialAdminPassword(%q) = %v, want %v", tc.pw, got, tc.insecure)
			}
		})
	}
}

func TestSQLiteDataDir(t *testing.T) {
	cases := []struct {
		name   string
		driver string
		dsn    string
		want   string
	}{
		{"relative default", "sqlite", "data/asiakirjat.db", "data"},
		{"absolute container path", "sqlite", "/app/data/asiakirjat.db", "/app/data"},
		{"bare filename needs no directory", "sqlite", "asiakirjat.db", ""},
		{"file URI with pragmas", "sqlite", "file:/app/data/a.db?_pragma=busy_timeout(5000)", "/app/data"},
		{"in-memory", "sqlite", ":memory:", ""},
		{"in-memory as file URI", "sqlite", "file::memory:", ""},
		{"empty driver defaults to sqlite", "", "data/asiakirjat.db", "data"},

		// A connection string is not a path. Deriving a directory from one
		// would create junk directories named after fragments of the DSN,
		// password included.
		{"postgres url", "postgres", "postgres://user:pw@db.example.com:5432/asiakirjat", ""},
		{"postgres keyword dsn", "postgres", "host=db.example.com user=a password=b dbname=c", ""},
		{"mysql", "mysql", "user:pw@tcp(db:3306)/asiakirjat?parseTime=true", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sqliteDataDir(tc.driver, tc.dsn); got != tc.want {
				t.Errorf("sqliteDataDir(%q, %q) = %q, want %q", tc.driver, tc.dsn, got, tc.want)
			}
		})
	}
}

func TestEnsureDirCreatesNestedPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data", "projects")

	if err := ensureDir(dir, "storage"); err != nil {
		t.Fatalf("ensureDir: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", dir)
	}

	// Idempotent: an existing directory is not an error.
	if err := ensureDir(dir, "storage"); err != nil {
		t.Errorf("ensureDir on existing directory: %v", err)
	}
}

func TestEnsureDirReportsWhyItFailed(t *testing.T) {
	// A regular file where a parent directory should be: the same shape as a
	// read-only mount or a directory owned by another user, both of which are
	// what actually happens in a container.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(blocker, "data")

	err := ensureDir(target, "database")
	if err == nil {
		t.Fatal("ensureDir returned nil for an uncreatable path")
	}
	// The message has to name the purpose and the path, or the operator is
	// left with the driver's misleading error instead.
	if !strings.Contains(err.Error(), "database") {
		t.Errorf("error does not name the purpose: %v", err)
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("error does not name the path: %v", err)
	}
}

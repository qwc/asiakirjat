package docs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemStorage(t *testing.T) {
	base := t.TempDir()
	storage := NewFilesystemStorage(base)

	if storage.BasePath() != base {
		t.Errorf("expected base path %s, got %s", base, storage.BasePath())
	}

	// ProjectPath
	pp := storage.ProjectPath("my-project")
	if pp != filepath.Join(base, "my-project") {
		t.Errorf("unexpected project path: %s", pp)
	}

	// VersionPath
	vp := storage.VersionPath("my-project", "v1.0")
	if vp != filepath.Join(base, "my-project", "v1.0") {
		t.Errorf("unexpected version path: %s", vp)
	}

	// VersionExists (doesn't exist yet)
	if storage.VersionExists("my-project", "v1.0") {
		t.Error("version should not exist yet")
	}

	// EnsureProjectDir
	if err := storage.EnsureProjectDir("my-project"); err != nil {
		t.Fatal(err)
	}

	// EnsureVersionDir
	if err := storage.EnsureVersionDir("my-project", "v1.0"); err != nil {
		t.Fatal(err)
	}

	if !storage.VersionExists("my-project", "v1.0") {
		t.Error("version should exist now")
	}

	// DeleteVersion
	if err := storage.DeleteVersion("my-project", "v1.0"); err != nil {
		t.Fatal(err)
	}

	if storage.VersionExists("my-project", "v1.0") {
		t.Error("version should be deleted")
	}
}

func TestMoveProject(t *testing.T) {
	base := t.TempDir()
	storage := NewFilesystemStorage(base)

	// Deploy a version under the old slug.
	if err := storage.EnsureVersionDir("old-slug", "v1.0"); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(storage.VersionPath("old-slug", "v1.0"), "index.html")
	if err := os.WriteFile(marker, []byte("<html>doc</html>"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := storage.MoveProject("old-slug", "new-slug"); err != nil {
		t.Fatalf("MoveProject: %v", err)
	}

	// Files must now live under the new slug and be gone from the old one.
	if _, err := os.Stat(storage.ProjectPath("old-slug")); !os.IsNotExist(err) {
		t.Errorf("old project dir should be gone, stat err = %v", err)
	}
	moved := filepath.Join(storage.VersionPath("new-slug", "v1.0"), "index.html")
	if _, err := os.Stat(moved); err != nil {
		t.Errorf("moved doc should exist at new path: %v", err)
	}
}

func TestMoveProjectNoSourceIsNoOp(t *testing.T) {
	storage := NewFilesystemStorage(t.TempDir())
	// Renaming a project that has never had a deployment must not error.
	if err := storage.MoveProject("never-deployed", "renamed"); err != nil {
		t.Errorf("expected no-op, got %v", err)
	}
	if _, err := os.Stat(storage.ProjectPath("renamed")); !os.IsNotExist(err) {
		t.Error("no directory should have been created")
	}
}

func TestMoveProjectRefusesExistingDestination(t *testing.T) {
	storage := NewFilesystemStorage(t.TempDir())
	if err := storage.EnsureProjectDir("source"); err != nil {
		t.Fatal(err)
	}
	if err := storage.EnsureProjectDir("taken"); err != nil {
		t.Fatal(err)
	}
	if err := storage.MoveProject("source", "taken"); err == nil {
		t.Error("expected error when destination already exists")
	}
	// Source must be left untouched so nothing is lost.
	if _, err := os.Stat(storage.ProjectPath("source")); err != nil {
		t.Errorf("source dir should be preserved on refusal: %v", err)
	}
}

func TestServeDoc(t *testing.T) {
	base := t.TempDir()

	// Create test files
	os.MkdirAll(filepath.Join(base, "sub"), 0755)
	os.WriteFile(filepath.Join(base, "index.html"), []byte("<html>root</html>"), 0644)
	os.WriteFile(filepath.Join(base, "sub", "page.html"), []byte("<html>sub</html>"), 0644)

	// Test isPathSafe
	if !isPathSafe(base, filepath.Join(base, "index.html")) {
		t.Error("path within base should be safe")
	}

	if isPathSafe(base, filepath.Join(base, "..", "etc", "passwd")) {
		t.Error("path outside base should not be safe")
	}
}

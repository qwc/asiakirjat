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

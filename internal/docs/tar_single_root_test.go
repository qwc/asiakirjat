package docs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// tarGz builds a .tar.gz from name → content pairs. A name ending in "/" is
// written as a directory entry.
func tarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()

	buf := new(bytes.Buffer)
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)

	for name, content := range entries {
		if name[len(name)-1] == '/' {
			if err := tw.WriteHeader(&tar.Header{
				Name: name, Mode: 0755, Typeflag: tar.TypeDir,
			}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0644, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func extractedFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestExtractTarKeepsTopLevelLayout covers audit item L-2. Tar extraction
// stripped the first path component off every entry unconditionally, while zip
// and 7z first check the archive really has one common root. A tarball whose
// files already sit at the top level therefore lost a real directory: the
// docs came out with guide/intro.html flattened to intro.html, breaking every
// link to it.
func TestExtractTarKeepsTopLevelLayout(t *testing.T) {
	dest := t.TempDir()

	archive := tarGz(t, map[string]string{
		"index.html":       "<html>home</html>",
		"guide/intro.html": "<html>intro</html>",
		"guide/setup.html": "<html>setup</html>",
		"assets/style.css": "body {}",
	})

	if err := ExtractArchive(bytes.NewReader(archive), "docs.tar.gz", dest); err != nil {
		t.Fatal(err)
	}

	got := extractedFiles(t, dest)
	want := map[string]string{
		"index.html":       "<html>home</html>",
		"guide/intro.html": "<html>intro</html>",
		"guide/setup.html": "<html>setup</html>",
		"assets/style.css": "body {}",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d files, got %v", len(want), got)
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("expected %s to be extracted with its directory intact, got %q", name, got[name])
		}
	}
}

// TestExtractTarStripsRealSingleRoot keeps the behaviour that made the
// stripping worthwhile: an archive wrapped in one directory is unwrapped, the
// same way zip archives are.
func TestExtractTarStripsRealSingleRoot(t *testing.T) {
	dest := t.TempDir()

	archive := tarGz(t, map[string]string{
		"docs/":                 "",
		"docs/index.html":       "<html>home</html>",
		"docs/guide/intro.html": "<html>intro</html>",
	})

	if err := ExtractArchive(bytes.NewReader(archive), "docs.tar.gz", dest); err != nil {
		t.Fatal(err)
	}

	got := extractedFiles(t, dest)
	want := map[string]string{
		"index.html":       "<html>home</html>",
		"guide/intro.html": "<html>intro</html>",
	}
	if len(got) != len(want) {
		t.Fatalf("expected the single root to be stripped, got %v", got)
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("expected %s, got %q", name, got[name])
		}
	}
}

// TestExtractTarWithMultipleRoots leaves an archive with several top-level
// directories exactly as it is.
func TestExtractTarWithMultipleRoots(t *testing.T) {
	dest := t.TempDir()

	archive := tarGz(t, map[string]string{
		"api/index.html":   "<html>api</html>",
		"guide/index.html": "<html>guide</html>",
	})

	if err := ExtractArchive(bytes.NewReader(archive), "docs.tar.gz", dest); err != nil {
		t.Fatal(err)
	}

	got := extractedFiles(t, dest)
	if got["api/index.html"] != "<html>api</html>" || got["guide/index.html"] != "<html>guide</html>" {
		t.Errorf("expected both roots preserved, got %v", got)
	}
}

// TestExtractTarRootSharingItsChildName guards the awkward case the unwrap
// has to stage around: a single root whose child has the same name.
func TestExtractTarRootSharingItsChildName(t *testing.T) {
	dest := t.TempDir()

	archive := tarGz(t, map[string]string{
		"docs/docs/index.html": "<html>nested</html>",
		"docs/readme.html":     "<html>readme</html>",
	})

	if err := ExtractArchive(bytes.NewReader(archive), "docs.tar.gz", dest); err != nil {
		t.Fatal(err)
	}

	got := extractedFiles(t, dest)
	want := map[string]string{
		"docs/index.html": "<html>nested</html>",
		"readme.html":     "<html>readme</html>",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("expected %s, got %q", name, got[name])
		}
	}
}

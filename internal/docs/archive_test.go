package docs

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractZip(t *testing.T) {
	dest := t.TempDir()

	// Create a zip in memory
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	// Add files
	f, _ := w.Create("index.html")
	f.Write([]byte("<html>hello</html>"))

	f, _ = w.Create("css/style.css")
	f.Write([]byte("body { color: red; }"))

	w.Close()

	err := ExtractArchive(bytes.NewReader(buf.Bytes()), "docs.zip", dest)
	if err != nil {
		t.Fatal(err)
	}

	// Verify files exist
	content, err := os.ReadFile(filepath.Join(dest, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "<html>hello</html>" {
		t.Errorf("unexpected content: %s", content)
	}

	content, err = os.ReadFile(filepath.Join(dest, "css", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "body { color: red; }" {
		t.Errorf("unexpected css content: %s", content)
	}
}

func TestExtractZipSingleRoot(t *testing.T) {
	dest := t.TempDir()

	// Create a zip with a single root directory
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	f, _ := w.Create("docs/index.html")
	f.Write([]byte("<html>hello</html>"))

	f, _ = w.Create("docs/page.html")
	f.Write([]byte("<html>page</html>"))

	w.Close()

	err := ExtractArchive(bytes.NewReader(buf.Bytes()), "docs.zip", dest)
	if err != nil {
		t.Fatal(err)
	}

	// Files should be at root level (single root stripped)
	content, err := os.ReadFile(filepath.Join(dest, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "<html>hello</html>" {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestExtractZipSlipPrevention(t *testing.T) {
	dest := t.TempDir()

	// Create a zip with path traversal
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	f, _ := w.Create("../../../etc/evil")
	f.Write([]byte("evil content"))

	w.Close()

	err := ExtractArchive(bytes.NewReader(buf.Bytes()), "evil.zip", dest)
	if err == nil {
		t.Error("expected zip-slip error")
	}
}

func TestExtractTarGz(t *testing.T) {
	dest := t.TempDir()

	// Create a tar.gz in memory
	buf := new(bytes.Buffer)
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)

	// Add file
	content := []byte("<html>tar content</html>")
	tw.WriteHeader(&tar.Header{
		Name: "docs/index.html",
		Mode: 0644,
		Size: int64(len(content)),
	})
	tw.Write(content)

	tw.Close()
	gw.Close()

	err := ExtractArchive(bytes.NewReader(buf.Bytes()), "docs.tar.gz", dest)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<html>tar content</html>" {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestExtractUnsupportedFormat(t *testing.T) {
	dest := t.TempDir()
	err := ExtractArchive(bytes.NewReader([]byte("not an archive")), "docs.rar", dest)
	if err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestWriteZipFromDir(t *testing.T) {
	// Create a temp directory with nested files
	srcDir := t.TempDir()
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "index.html"), []byte("<html>hello</html>"), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub", "page.html"), []byte("<html>page</html>"), 0644)

	// Write zip to buffer
	var buf bytes.Buffer
	if err := WriteZipFromDir(&buf, srcDir); err != nil {
		t.Fatal(err)
	}

	// Read back and verify
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	files := make(map[string]string)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(data)
	}

	if len(files) != 2 {
		t.Errorf("expected 2 files in zip, got %d", len(files))
	}
	if files["index.html"] != "<html>hello</html>" {
		t.Errorf("unexpected index.html content: %s", files["index.html"])
	}
	if files["sub/page.html"] != "<html>page</html>" {
		t.Errorf("unexpected sub/page.html content: %s", files["sub/page.html"])
	}
}

func TestWriteZipFromDirEmpty(t *testing.T) {
	srcDir := t.TempDir()

	var buf bytes.Buffer
	if err := WriteZipFromDir(&buf, srcDir); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	if len(zr.File) != 0 {
		t.Errorf("expected 0 files in zip, got %d", len(zr.File))
	}
}

func TestIsPathSafe(t *testing.T) {
	tests := []struct {
		base   string
		target string
		safe   bool
	}{
		{"/data", "/data/file.txt", true},
		{"/data", "/data/sub/file.txt", true},
		{"/data", "/data", true},
		{"/data", "/etc/passwd", false},
		{"/data", "/data/../etc/passwd", false},
	}

	for _, tt := range tests {
		got := isPathSafe(tt.base, tt.target)
		if got != tt.safe {
			t.Errorf("isPathSafe(%q, %q) = %v, want %v", tt.base, tt.target, got, tt.safe)
		}
	}
}

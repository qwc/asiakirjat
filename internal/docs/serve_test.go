package docs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Regression test for audit finding H-8: the prefix check on
// strings.HasPrefix(absFile, absStorage) was not separator-aware, so a
// version directory like /data/proj/v1 prefix-matched its sibling
// /data/proj/v10. With the fix, a request that resolves to a sibling
// directory must be rejected.
func TestServeDocRejectsSiblingPrefixLeak(t *testing.T) {
	base := t.TempDir()

	// Two sibling directories whose names share a prefix.
	v1 := filepath.Join(base, "v1")
	v10 := filepath.Join(base, "v10")
	if err := os.MkdirAll(v1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(v10, 0755); err != nil {
		t.Fatal(err)
	}
	// Plant a recognizable file in v10 only.
	if err := os.WriteFile(filepath.Join(v10, "secret.html"), []byte("LEAK"), 0644); err != nil {
		t.Fatal(err)
	}

	// Caller has access to v1 only. Try to escape into the sibling v10
	// using a path that, after filepath.Clean, ends up at v10/secret.html.
	// "../v10/secret.html" relative to base "v1" would resolve into v10.
	req := httptest.NewRequest("GET", "/anything", nil)
	w := httptest.NewRecorder()
	ServeDoc(w, req, v1, "../v10/secret.html")

	if w.Code == http.StatusOK {
		body := w.Body.String()
		t.Errorf("sibling-directory traversal leaked content: status=%d body=%q", w.Code, body)
	}
	if w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Errorf("expected 403 or 404 for sibling-escape, got %d", w.Code)
	}
}

// Sanity: a normal request inside the storage path still works.
func TestServeDocAllowsLegitimateRequest(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "index.html"), []byte("OK"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ServeDoc(w, req, base, "index.html")

	if w.Code != http.StatusOK {
		t.Errorf("legitimate request rejected, got %d", w.Code)
	}
	if w.Body.String() != "OK" {
		t.Errorf("unexpected body: %q", w.Body.String())
	}
}

// Storage path itself (filePath="") must resolve and serve index.html.
func TestServeDocAllowsRootDirectory(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "index.html"), []byte("ROOT"), 0644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ServeDoc(w, req, base, "")

	if w.Code != http.StatusOK {
		t.Errorf("root directory request rejected, got %d", w.Code)
	}
	if w.Body.String() != "ROOT" {
		t.Errorf("unexpected body: %q", w.Body.String())
	}
}

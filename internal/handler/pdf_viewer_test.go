package handler

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
)

// Regression test for audit finding C-2: the PDF viewer wrapper used
// fmt.Fprintf and injected projectName and version unescaped into HTML.
// The fix moves the wrapper to html/template, which auto-escapes string
// substitutions. Version tags are now validated upstream (C-1), so this
// test focuses on the still-free-form project name field.
func TestPDFViewerEscapesProjectName(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	// Need a real user for the version's uploaded_by FK.
	uploader := &database.User{Username: "pdf-uploader", AuthSource: "builtin", Role: "editor"}
	if err := app.handler.users.Create(ctx, uploader); err != nil {
		t.Fatal(err)
	}

	// Create a project whose name contains an XSS payload. Slug is constrained
	// by validation, so put the payload in Name (which is free-form).
	xssPayload := `</title><script>window.__xss=1</script>`
	project := &database.Project{
		Slug:       "pdf-xss-proj",
		Name:       xssPayload,
		Visibility: "public",
	}
	if err := app.handler.projects.Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	// Create a PDF version on disk + DB row so the viewer renders.
	versionDir := filepath.Join(app.handler.storage.VersionPath("pdf-xss-proj", "v1"))
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "document.pdf"), []byte("%PDF-1.4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := app.handler.versions.Create(ctx, &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1",
		StoragePath: versionDir,
		ContentType: "pdf",
		UploadedBy:  &uploader.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// Fetch the viewer wrapper (any path other than document.pdf triggers it).
	resp, err := http.Get(app.server.URL + "/project/pdf-xss-proj/v1/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// The payload, raw, must not appear. The script-tag form is the clear test.
	if strings.Contains(bodyStr, "<script>window.__xss=1</script>") {
		t.Errorf("PDF viewer rendered raw <script> from project name; XSS regression. Body excerpt: %s", bodyStr[:min(len(bodyStr), 500)])
	}
	// </title> must not appear from user input — only the literal one from the template.
	if strings.Count(bodyStr, "</title>") != 1 {
		t.Errorf("expected exactly one </title> in response, got %d (likely XSS regression)", strings.Count(bodyStr, "</title>"))
	}
	// The escaped form should appear (proof the project name was rendered, just safely).
	if !strings.Contains(bodyStr, "&lt;/title&gt;") && !strings.Contains(bodyStr, "&lt;script&gt;") {
		t.Errorf("expected an HTML-escaped form of the payload in the response; body excerpt: %s", bodyStr[:min(len(bodyStr), 500)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

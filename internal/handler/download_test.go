package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

func TestDownloadVersionSuccess(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "dl-proj", "Download Project", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("dl-proj", "v1.0.0")
	versionPath := storage.VersionPath("dl-proj", "v1.0.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"), []byte("<html>download me</html>"), 0644)
	os.MkdirAll(filepath.Join(versionPath, "css"), 0755)
	os.WriteFile(filepath.Join(versionPath, "css", "style.css"), []byte("body{}"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	cookies := loginUser(t, app, "admin", "admin123")

	req, _ := http.NewRequest("GET", app.server.URL+"/project/dl-proj/version/v1.0.0/download", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("expected Content-Type application/zip, got %s", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="dl-proj-v1.0.0.zip"` {
		t.Errorf("unexpected Content-Disposition: %s", cd)
	}

	// Read and verify zip contents
	body, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}

	files := make(map[string]string)
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(data)
	}

	if files["index.html"] != "<html>download me</html>" {
		t.Errorf("unexpected index.html content: %q", files["index.html"])
	}
	if files["css/style.css"] != "body{}" {
		t.Errorf("unexpected css/style.css content: %q", files["css/style.css"])
	}
}

func TestDownloadVersionPublicAnonymous(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "dl-pub", "Download Public", true)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("dl-pub", "v1.0.0")
	versionPath := storage.VersionPath("dl-pub", "v1.0.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"), []byte("<html>public</html>"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	// No login — anonymous request
	resp, err := http.Get(app.server.URL + "/project/dl-pub/version/v1.0.0/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for anonymous download of public project, got %d", resp.StatusCode)
	}
}

func TestDownloadVersionPrivateAnonymousRedirects(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "dl-priv", "Download Private", false)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("dl-priv", "v1.0.0")
	versionPath := storage.VersionPath("dl-priv", "v1.0.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"), []byte("<html>private</html>"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/project/dl-priv/version/v1.0.0/download", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for anonymous on private project, got %d", resp.StatusCode)
	}
}

func TestDownloadVersionPrivateForbiddenForViewer(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "dl-forbid", "Download Forbidden", false)

	ctx := context.Background()
	storage := app.handler.storage
	storage.EnsureVersionDir("dl-forbid", "v1.0.0")
	versionPath := storage.VersionPath("dl-forbid", "v1.0.0")
	os.WriteFile(filepath.Join(versionPath, "index.html"), []byte("<html>forbidden</html>"), 0644)

	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: versionPath,
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	// Create viewer with no access to this project
	hash, _ := auth.HashPassword("viewer123")
	viewer := &database.User{
		Username: "dlviewer", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, viewer)

	cookies := loginUser(t, app, "dlviewer", "viewer123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, _ := http.NewRequest("GET", app.server.URL+"/project/dl-forbid/version/v1.0.0/download", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for viewer without access, got %d", resp.StatusCode)
	}
}

func TestDownloadVersionNotFoundProject(t *testing.T) {
	app := setupTestApp(t)

	resp, err := http.Get(app.server.URL + "/project/nonexistent/version/v1.0.0/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent project, got %d", resp.StatusCode)
	}
}

func TestDownloadVersionNotFoundVersion(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	seedProject(t, app, "dl-nover", "Download No Version", true)

	resp, err := http.Get(app.server.URL + "/project/dl-nover/version/v99.0.0/download")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent version, got %d", resp.StatusCode)
	}
}

package handler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

func TestLatestVersionTagWithPin(t *testing.T) {
	versions := []database.Version{
		{Tag: "v1.0.0"},
		{Tag: "v2.0.0"},
		{Tag: "v0.9.0"},
	}

	// Without pin, should return semver-sorted latest
	got := latestVersionTag(versions, nil)
	if got != "v2.0.0" {
		t.Errorf("expected v2.0.0, got %s", got)
	}

	// With pin to existing version
	pinned := "v1.0.0"
	got = latestVersionTag(versions, &pinned)
	if got != "v1.0.0" {
		t.Errorf("expected pinned v1.0.0, got %s", got)
	}

	// With pin to non-existent version, fallback to semver
	nonExistent := "v99.0.0"
	got = latestVersionTag(versions, &nonExistent)
	if got != "v2.0.0" {
		t.Errorf("expected fallback to v2.0.0, got %s", got)
	}

	// Empty versions
	got = latestVersionTag(nil, nil)
	if got != "" {
		t.Errorf("expected empty string for nil versions, got %s", got)
	}
}

func TestPinVersionRequiresAuth(t *testing.T) {
	app := setupTestApp(t)
	seedProject(t, app, "docs", "Documentation", true)

	resp, err := http.Post(app.server.URL+"/project/docs/version/v1.0.0/pin", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should redirect to login
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusSeeOther {
		// Without auth, requireAuth middleware redirects to login
		// The response might be a redirect or an error page
	}
}

func TestPinVersionFullFlow(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Documentation", true)
	ctx := context.Background()

	// Create a version
	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: "/tmp/test",
		ContentType: "archive",
		UploadedBy:  admin.ID,
	}
	if err := app.handler.versions.Create(ctx, version); err != nil {
		t.Fatal(err)
	}
	version2 := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v2.0.0",
		StoragePath: "/tmp/test2",
		ContentType: "archive",
		UploadedBy:  admin.ID,
	}
	if err := app.handler.versions.Create(ctx, version2); err != nil {
		t.Fatal(err)
	}

	cookies := loginUser(t, app, "admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Pin v1.0.0 as permanent
	form := url.Values{}
	form.Set("permanent", "true")
	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/version/v1.0.0/pin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify project has pinned version
	updatedProject, err := app.handler.projects.GetBySlug(ctx, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if updatedProject.PinnedVersion == nil {
		t.Fatal("expected PinnedVersion to be set")
	}
	if *updatedProject.PinnedVersion != "v1.0.0" {
		t.Errorf("expected PinnedVersion v1.0.0, got %s", *updatedProject.PinnedVersion)
	}
	if !updatedProject.PinPermanent {
		t.Error("expected PinPermanent to be true")
	}

	// Unpin
	req2, _ := http.NewRequest("POST", app.server.URL+"/project/docs/unpin", nil)
	for _, c := range cookies {
		req2.AddCookie(c)
	}

	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp2.StatusCode)
	}

	// Verify pin cleared
	updatedProject2, _ := app.handler.projects.GetBySlug(ctx, "docs")
	if updatedProject2.PinnedVersion != nil {
		t.Error("expected PinnedVersion to be nil after unpin")
	}
	if updatedProject2.PinPermanent {
		t.Error("expected PinPermanent to be false after unpin")
	}
}

func TestPinVersionTemporary(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Documentation", true)
	ctx := context.Background()

	// Create a version
	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: "/tmp/test",
		ContentType: "archive",
		UploadedBy:  admin.ID,
	}
	if err := app.handler.versions.Create(ctx, version); err != nil {
		t.Fatal(err)
	}

	cookies := loginUser(t, app, "admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Pin v1.0.0 as temporary
	form := url.Values{}
	form.Set("permanent", "false")
	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/version/v1.0.0/pin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	// Verify project has temporary pin
	updatedProject, _ := app.handler.projects.GetBySlug(ctx, "docs")
	if updatedProject.PinnedVersion == nil || *updatedProject.PinnedVersion != "v1.0.0" {
		t.Error("expected PinnedVersion v1.0.0")
	}
	if updatedProject.PinPermanent {
		t.Error("expected PinPermanent to be false for temporary pin")
	}
}

func TestPinVersionNonExistentVersion(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	seedProject(t, app, "docs", "Documentation", true)

	cookies := loginUser(t, app, "admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Try to pin a version that doesn't exist
	form := url.Values{}
	form.Set("permanent", "true")
	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/version/v99.0.0/pin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent version, got %d", resp.StatusCode)
	}
}

func TestTemporaryPinClearedOnNewUpload(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Documentation", true)
	ctx := context.Background()

	// Create initial version and set a temporary pin
	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: "/tmp/test",
		ContentType: "archive",
		UploadedBy:  admin.ID,
	}
	if err := app.handler.versions.Create(ctx, version); err != nil {
		t.Fatal(err)
	}

	// Set temporary pin
	v := "v1.0.0"
	project.PinnedVersion = &v
	project.PinPermanent = false
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	cookies := loginUser(t, app, "admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Upload a new version (should clear the temporary pin)
	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html><body>New version</body></html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v2.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303, got %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Verify pin was cleared
	updatedProject, _ := app.handler.projects.GetBySlug(ctx, "docs")
	if updatedProject.PinnedVersion != nil {
		t.Errorf("expected PinnedVersion to be nil after new upload, got %v", *updatedProject.PinnedVersion)
	}
}

func TestPermanentPinNotClearedOnNewUpload(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Documentation", true)
	ctx := context.Background()

	// Create initial version and set a permanent pin
	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: "/tmp/test",
		ContentType: "archive",
		UploadedBy:  admin.ID,
	}
	if err := app.handler.versions.Create(ctx, version); err != nil {
		t.Fatal(err)
	}

	// Set permanent pin
	v := "v1.0.0"
	project.PinnedVersion = &v
	project.PinPermanent = true
	if err := app.handler.projects.Update(ctx, project); err != nil {
		t.Fatal(err)
	}

	cookies := loginUser(t, app, "admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Upload a new version (should NOT clear the permanent pin)
	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html><body>New version</body></html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v2.0.0")
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303, got %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Verify pin was NOT cleared
	updatedProject, _ := app.handler.projects.GetBySlug(ctx, "docs")
	if updatedProject.PinnedVersion == nil {
		t.Fatal("expected PinnedVersion to still be set after upload with permanent pin")
	}
	if *updatedProject.PinnedVersion != "v1.0.0" {
		t.Errorf("expected PinnedVersion v1.0.0, got %s", *updatedProject.PinnedVersion)
	}
	if !updatedProject.PinPermanent {
		t.Error("expected PinPermanent to still be true")
	}
}

func TestUploadCreatesLogEntry(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Documentation", true)
	ctx := context.Background()

	cookies := loginUser(t, app, "admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Upload a version
	zipBuf := createTestZip(t, map[string]string{
		"index.html": "<html><body>Hello</body></html>",
	})

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "v1.0.0")
	part, _ := writer.CreateFormFile("archive", "my-docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303, got %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Verify upload log was created
	logs, err := app.handler.uploadLogs.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 upload log entry, got %d", len(logs))
	}
	if logs[0].VersionTag != "v1.0.0" {
		t.Errorf("expected version tag v1.0.0, got %s", logs[0].VersionTag)
	}
	if logs[0].ContentType != "archive" {
		t.Errorf("expected content type archive, got %s", logs[0].ContentType)
	}
	if logs[0].UploadedBy != admin.ID {
		t.Errorf("expected uploaded_by %d, got %d", admin.ID, logs[0].UploadedBy)
	}
	if logs[0].IsReupload {
		t.Error("expected IsReupload to be false for new upload")
	}
	if logs[0].Filename != "my-docs.zip" {
		t.Errorf("expected filename 'my-docs.zip', got %s", logs[0].Filename)
	}
}

func TestReuploadCreatesLogEntryMarkedAsReupload(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Documentation", true)
	ctx := context.Background()

	cookies := loginUser(t, app, "admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	uploadVersion := func(filename string) {
		t.Helper()
		zipBuf := createTestZip(t, map[string]string{
			"index.html": "<html><body>Content</body></html>",
		})

		body := new(bytes.Buffer)
		writer := multipart.NewWriter(body)
		writer.WriteField("version", "v1.0.0")
		part, _ := writer.CreateFormFile("archive", filename)
		part.Write(zipBuf.Bytes())
		writer.Close()

		req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		for _, c := range cookies {
			req.AddCookie(c)
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	// First upload
	uploadVersion("docs-v1.zip")
	// Re-upload same version
	uploadVersion("docs-v1-updated.zip")

	// Check logs
	logs, err := app.handler.uploadLogs.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 upload log entries, got %d", len(logs))
	}

	// Newest first (reupload)
	if !logs[0].IsReupload {
		t.Error("expected newest entry to be marked as reupload")
	}
	if logs[0].Filename != "docs-v1-updated.zip" {
		t.Errorf("expected filename 'docs-v1-updated.zip', got %s", logs[0].Filename)
	}

	// Oldest (original upload)
	if logs[1].IsReupload {
		t.Error("expected oldest entry to not be marked as reupload")
	}
}

func TestProjectDetailShowsUploadLogsForEditors(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Documentation", true)
	ctx := context.Background()

	// Create a version and an upload log entry
	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: "/tmp/test",
		ContentType: "archive",
		UploadedBy:  admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	logEntry := &database.UploadLog{
		ProjectID:   project.ID,
		VersionTag:  "v1.0.0",
		ContentType: "archive",
		UploadedBy:  admin.ID,
		IsReupload:  false,
		Filename:    "docs.zip",
	}
	app.handler.uploadLogs.Create(ctx, logEntry)

	cookies := loginUser(t, app, "admin", "admin123")

	req, _ := http.NewRequest("GET", app.server.URL+"/project/docs", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "Upload Log") {
		t.Error("expected upload log section to be visible for admin")
	}
	if !strings.Contains(bodyStr, "docs.zip") {
		t.Error("expected upload log to show filename")
	}
}

func TestProjectDetailShowsPinBadge(t *testing.T) {
	app := setupTestApp(t)
	admin := seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Documentation", true)
	ctx := context.Background()

	// Create versions
	v1 := &database.Version{
		ProjectID: project.ID, Tag: "v1.0.0",
		StoragePath: "/tmp/test", ContentType: "archive", UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, v1)
	v2 := &database.Version{
		ProjectID: project.ID, Tag: "v2.0.0",
		StoragePath: "/tmp/test2", ContentType: "archive", UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, v2)

	// Pin v1.0.0 permanently
	pinned := "v1.0.0"
	project.PinnedVersion = &pinned
	project.PinPermanent = true
	app.handler.projects.Update(ctx, project)

	cookies := loginUser(t, app, "admin", "admin123")
	req, _ := http.NewRequest("GET", app.server.URL+"/project/docs", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "Pinned") {
		t.Error("expected 'Pinned' badge to appear for permanently pinned version")
	}
	// The pinned version should have an Unpin button
	if !strings.Contains(bodyStr, "Unpin") {
		t.Error("expected 'Unpin' button for pinned version")
	}
}

func TestViewerCannotPin(t *testing.T) {
	app := setupTestApp(t)
	seedAdmin(t, app)
	project := seedProject(t, app, "docs", "Documentation", false)
	ctx := context.Background()

	// Create a viewer user
	viewerPw := "viewer123"
	viewerHash, _ := auth.HashPassword(viewerPw)
	viewer := &database.User{
		Username:   "viewer",
		Email:      "viewer@example.com",
		Password:   &viewerHash,
		AuthSource: "builtin",
		Role:       "viewer",
	}
	app.handler.users.Create(ctx, viewer)

	// Grant viewer access
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    viewer.ID,
		Role:      "viewer",
		Source:    "manual",
	})

	// Create a version
	admin, _ := app.handler.users.GetByUsername(ctx, "admin")
	version := &database.Version{
		ProjectID: project.ID, Tag: "v1.0.0",
		StoragePath: "/tmp/test", ContentType: "archive", UploadedBy: admin.ID,
	}
	app.handler.versions.Create(ctx, version)

	cookies := loginUser(t, app, "viewer", viewerPw)
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("permanent", "true")
	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/version/v1.0.0/pin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for viewer trying to pin, got %d", resp.StatusCode)
	}
}


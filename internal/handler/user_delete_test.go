package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Regression test for audit finding H-4: deleting a user who has uploaded
// any version (or any upload_log row) must succeed. Before the migration,
// versions.uploaded_by + upload_logs.uploaded_by had FK without ON DELETE,
// so the user delete failed with a FK constraint error and the admin UI
// had no way to clean up the account.
func TestAdminCanDeleteUserAfterUploads(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	// Seed an admin to perform the delete.
	adminHash, _ := auth.HashPassword("admin123")
	admin := &database.User{
		Username: "h4-admin", Password: &adminHash,
		AuthSource: "builtin", Role: "admin",
	}
	app.handler.users.Create(ctx, admin)

	// Seed an editor who will upload, and then be deleted.
	editor := &database.User{
		Username: "h4-doomed-uploader", AuthSource: "builtin", Role: "editor",
	}
	app.handler.users.Create(ctx, editor)

	// Seed a project and a version attributed to the editor.
	project := &database.Project{Slug: "h4-proj", Name: "H4", Visibility: "public"}
	app.handler.projects.Create(ctx, project)

	editorID := editor.ID
	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1",
		StoragePath: "/tmp/notreal",
		ContentType: "archive",
		UploadedBy:  &editorID,
	}
	if err := app.handler.versions.Create(ctx, version); err != nil {
		t.Fatal(err)
	}

	uploadLog := &database.UploadLog{
		ProjectID:   project.ID,
		VersionTag:  "v1",
		ContentType: "archive",
		UploadedBy:  &editorID,
		Filename:    "x.zip",
	}
	if err := app.handler.uploadLogs.Create(ctx, uploadLog); err != nil {
		t.Fatal(err)
	}

	// Now delete the editor via the admin handler.
	cookies := loginUser(t, app, "h4-admin", "admin123")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("%s/admin/users/%d/delete", app.server.URL, editorID), nil)
	req.Header.Set("X-CSRF-Token", csrfTokenFor(t, app, cookies))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 on user delete with uploads, got %d (FK regression?)", resp.StatusCode)
	}

	// Confirm the editor is gone.
	if _, err := app.handler.users.GetByID(ctx, editorID); err == nil {
		t.Error("editor should no longer exist after delete")
	}

	// Confirm the version row still exists (ON DELETE SET NULL).
	v, err := app.handler.versions.GetByProjectAndTag(ctx, project.ID, "v1")
	if err != nil {
		t.Fatalf("version should still exist after uploader deleted: %v", err)
	}
	if v.UploadedBy != nil {
		t.Errorf("expected uploaded_by to be NULL after uploader delete, got %v", *v.UploadedBy)
	}

	// Confirm the upload log row still exists with NULL uploaded_by too.
	logs, err := app.handler.uploadLogs.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 upload log, got %d", len(logs))
	}
	if logs[0].UploadedBy != nil {
		t.Errorf("expected upload_log.uploaded_by NULL after uploader delete, got %v", *logs[0].UploadedBy)
	}
}

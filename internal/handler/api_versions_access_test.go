package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Regression test for audit finding C-4: GET /api/project/{slug}/versions
// must not leak version metadata for projects the caller can't view.
func TestAPIVersionsHidesNonViewableProjects(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	uploader := &database.User{Username: "vlist-up", AuthSource: "builtin", Role: "editor"}
	app.handler.users.Create(ctx, uploader)

	// Three projects: public, private (no grant), custom (no grant).
	pub := &database.Project{Slug: "pub-vlist", Name: "Pub", Visibility: "public"}
	app.handler.projects.Create(ctx, pub)
	priv := &database.Project{Slug: "priv-vlist", Name: "Priv", Visibility: "private"}
	app.handler.projects.Create(ctx, priv)
	cust := &database.Project{Slug: "cust-vlist", Name: "Cust", Visibility: "custom"}
	app.handler.projects.Create(ctx, cust)

	// Seed each with one version so a non-200 response unambiguously means
	// "blocked" rather than "no rows".
	uid := uploader.ID
	for _, p := range []*database.Project{pub, priv, cust} {
		if err := app.handler.versions.Create(ctx, &database.Version{
			ProjectID:   p.ID,
			Tag:         "v1",
			StoragePath: "/tmp/notreal",
			ContentType: "archive",
			UploadedBy:  &uid,
		}); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name       string
		slug       string
		wantStatus int
	}{
		{"public is reachable anonymously", "pub-vlist", http.StatusOK},
		{"private is hidden from anonymous", "priv-vlist", http.StatusNotFound},
		{"custom is hidden from anonymous", "cust-vlist", http.StatusNotFound},
		{"nonexistent slug is 404", "no-such-slug", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(app.server.URL + "/api/project/" + tc.slug + "/versions")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("got %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

// Sanity: a logged-in viewer with a per-project grant on a custom-visibility
// project CAN see the version list. Confirms the access check doesn't
// over-block users who do have access. (Uses custom visibility so this test
// is independent of whether private-visibility projects honor ProjectAccess.)
func TestAPIVersionsVisibleToGrantedViewer(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	hash, _ := auth.HashPassword("viewer123")
	viewer := &database.User{
		Username: "vlist-viewer", Password: &hash,
		AuthSource: "builtin", Role: "viewer",
	}
	app.handler.users.Create(ctx, viewer)

	uploader := &database.User{Username: "vlist-up2", AuthSource: "builtin", Role: "editor"}
	app.handler.users.Create(ctx, uploader)

	cust := &database.Project{Slug: "granted-cust", Name: "Cust", Visibility: "custom"}
	app.handler.projects.Create(ctx, cust)
	app.handler.access.Grant(ctx, &database.ProjectAccess{
		ProjectID: cust.ID, UserID: viewer.ID, Role: "viewer",
	})
	upID := uploader.ID
	app.handler.versions.Create(ctx, &database.Version{
		ProjectID:   cust.ID,
		Tag:         "v1",
		StoragePath: "/tmp/notreal",
		ContentType: "archive",
		UploadedBy:  &upID,
	})

	cookies := loginUser(t, app, "vlist-viewer", "viewer123")
	req, _ := http.NewRequest("GET", app.server.URL+"/api/project/granted-cust/versions", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("granted viewer should see version list, got %d", resp.StatusCode)
	}
}

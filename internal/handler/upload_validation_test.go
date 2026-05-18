package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Regression test for audit finding C-1: malicious version tags that contain
// path-traversal sequences must be rejected before any filesystem call.
func TestAPIUploadRejectsTraversalInVersionTag(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	robot := &database.User{
		Username: "traversal-bot", AuthSource: "robot", Role: "editor", IsRobot: true,
	}
	app.handler.users.Create(ctx, robot)

	project := &database.Project{Slug: "traversal-proj", Name: "Traversal", Visibility: "public"}
	app.handler.projects.Create(ctx, project)

	rawToken, _ := auth.GenerateToken(32)
	tokenHash := auth.HashToken(rawToken)
	app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: robot.ID, TokenHash: tokenHash, Name: "t", Scopes: "upload",
	})

	maliciousTags := []string{
		"../escape",
		"../../etc/passwd",
		"..",
		".",
		".hidden",
		"v1/v2",
		"v1\\v2",
		"v1\nv2",
		"v1\"injected",
		"",
	}

	for _, tag := range maliciousTags {
		t.Run("tag="+tag, func(t *testing.T) {
			zipBuf := createTestZip(t, map[string]string{"index.html": "x"})
			body := new(bytes.Buffer)
			writer := multipart.NewWriter(body)
			writer.WriteField("version", tag)
			part, _ := writer.CreateFormFile("archive", "docs.zip")
			part.Write(zipBuf.Bytes())
			writer.Close()

			req, _ := http.NewRequest("POST", app.server.URL+"/api/project/traversal-proj/upload", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.Header.Set("Authorization", "Bearer "+rawToken)

			resp, err := (&http.Client{}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("malicious version tag %q must be rejected with 400, got %d", tag, resp.StatusCode)
			}
		})
	}
}

// Regression test for audit finding M-5: admin form must reject invalid slugs
// (containing path separators, uppercase, etc).
func TestAdminCreateProjectRejectsInvalidSlug(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	adminHash, _ := auth.HashPassword("admin123")
	admin := &database.User{
		Username: "validation-admin", Password: &adminHash,
		AuthSource: "builtin", Role: "admin",
	}
	app.handler.users.Create(ctx, admin)
	cookies := loginUser(t, app, "validation-admin", "admin123")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	badSlugs := []string{
		"../escape",
		"a/b",
		"UPPER",
		"with space",
		"with_underscore",
		"",
		"-leading",
		"trailing-",
	}

	for _, slug := range badSlugs {
		t.Run("slug="+slug, func(t *testing.T) {
			form := "slug=" + slug + "&name=test&visibility=custom"
			req, _ := http.NewRequest("POST", app.server.URL+"/admin/projects",
				bytes.NewBufferString(form))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for _, c := range cookies {
				req.AddCookie(c)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("invalid slug %q must be rejected with 400, got %d", slug, resp.StatusCode)
			}
		})
	}
}

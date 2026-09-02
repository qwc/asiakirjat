package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Issue #155. A token scoped to one project must not be able to bring new
// projects into existence: the auto-create path on upload used unscoped
// authentication because there was "no project to scope to", which turned
// every project token into a general-purpose one on any slug that did not
// exist yet.
func TestProjectScopedTokenCannotAutoCreateProjects(t *testing.T) {
	app := setupTestApp(t)
	app.handler.config.Projects.AutoCreate = true
	ctx := context.Background()

	robot := &database.User{
		Username: "scoped-bot", AuthSource: "robot", Role: "editor", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}
	owned := seedProject(t, app, "owned", "Owned", true)

	rawToken, _ := auth.GenerateToken(32)
	if err := app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: robot.ID, ProjectID: &owned.ID,
		TokenHash: auth.HashToken(rawToken), Name: "ci", Scopes: "upload",
	}); err != nil {
		t.Fatal(err)
	}

	// Both upload endpoints reach the same auto-create branch.
	for _, tc := range []struct{ name, path, slug string }{
		{"path endpoint", "/api/project/sneaky-one/upload", ""},
		{"general endpoint", "/api/upload", "sneaky-two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := apiUpload(t, app, tc.path, rawToken, tc.slug, "v1.0.0")
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("expected 403 for a project-scoped token, got %d: %s", resp.StatusCode, body)
			}
			for _, slug := range []string{"sneaky-one", "sneaky-two"} {
				if _, err := app.handler.projects.GetBySlug(ctx, slug); err == nil {
					t.Errorf("project %q was created by a token scoped to another project", slug)
				}
			}
		})
	}

	// The token still does the job it was issued for.
	resp := apiUpload(t, app, "/api/project/owned/upload", rawToken, "", "v1.0.0")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected the scoped token to still upload to its own project, got %d: %s", resp.StatusCode, body)
	}
}

// A global (unscoped) token is what auto-create is for, so it must keep
// working — the fix above must not close the feature itself.
func TestGlobalTokenStillAutoCreates(t *testing.T) {
	app := setupTestApp(t)
	app.handler.config.Projects.AutoCreate = true
	ctx := context.Background()

	robot := &database.User{
		Username: "global-bot", AuthSource: "robot", Role: "editor", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}
	rawToken, _ := auth.GenerateToken(32)
	if err := app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: robot.ID, TokenHash: auth.HashToken(rawToken), Name: "ci", Scopes: "upload,create",
	}); err != nil {
		t.Fatal(err)
	}

	resp := apiUpload(t, app, "/api/project/fresh-proj/upload", rawToken, "", "v1.0.0")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if _, err := app.handler.projects.GetBySlug(ctx, "fresh-proj"); err != nil {
		t.Error("expected the global token to auto-create the project")
	}
}

// The revoke button on Admin > Robots posted to a URL built from a field that
// does not exist at that point in the template, so the id came out empty and
// the request never reached the handler. The handler itself was tested with a
// hand-built URL, which is why nobody noticed.
func TestRobotTokenRevokeLinkPointsAtTheHandler(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	robot := &database.User{
		Username: "link-bot", AuthSource: "robot", Role: "editor", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}
	token := &database.APIToken{
		UserID: robot.ID, TokenHash: "link-hash", Name: "ci", Scopes: "upload",
	}
	if err := app.handler.tokens.Create(ctx, token); err != nil {
		t.Fatal(err)
	}

	_, body := adminGet(t, app, cookies, "/admin/robots")
	want := fmt.Sprintf("/admin/robots/%d/tokens/%d/revoke", robot.ID, token.ID)
	if !strings.Contains(body, want) {
		t.Fatalf("expected the revoke form to post to %q\npage: %s", want, body)
	}

	// And the URL the page renders actually revokes.
	resp := adminPost(t, app, cookies, want, url.Values{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 from the rendered revoke URL, got %d", resp.StatusCode)
	}
	if _, err := app.handler.tokens.GetByID(ctx, token.ID); err == nil {
		t.Error("expected the token to be gone after revoking")
	}
}

// The robots page mints tokens for whatever user id is in the URL. A token for
// a human account would authenticate as that person — including an admin — so
// the endpoint has to insist on an actual robot.
func TestGenerateTokenRefusesNonRobotUsers(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	admin := seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	resp := adminPost(t, app, cookies, fmt.Sprintf("/admin/robots/%d/tokens", admin.ID),
		url.Values{"name": {"laundered"}})
	defer resp.Body.Close()

	if resp.StatusCode < 400 {
		t.Errorf("expected the request to be refused, got %d", resp.StatusCode)
	}
	tokens, err := app.handler.tokens.ListByUser(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected no token for a human account, got %d", len(tokens))
	}
}

// A token scoped to a project that does not exist can never authenticate
// anything: it is a typo that only shows up as a 401 in CI a week later.
func TestGenerateTokenRefusesUnknownProject(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	robot := &database.User{
		Username: "typo-bot", AuthSource: "robot", Role: "editor", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}

	resp := adminPost(t, app, cookies, fmt.Sprintf("/admin/robots/%d/tokens", robot.ID),
		url.Values{"name": {"ci"}, "project_id": {"424242"}})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for an unknown project, got %d", resp.StatusCode)
	}
	tokens, err := app.handler.tokens.ListByUser(ctx, robot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("expected no token for an unknown project, got %d", len(tokens))
	}
}

// apiUpload posts a one-file archive to an upload endpoint with a bearer
// token. A non-empty slug is sent as the "project" field the general endpoint
// reads; the path endpoint carries it in the URL instead.
func apiUpload(t *testing.T, app *testApp, path, rawToken, slug, version string) *http.Response {
	t.Helper()

	zipBuf := createTestZip(t, map[string]string{"index.html": "<html>hi</html>"})
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	if slug != "" {
		writer.WriteField("project", slug)
	}
	writer.WriteField("version", version)
	part, _ := writer.CreateFormFile("archive", "docs.zip")
	part.Write(zipBuf.Bytes())
	writer.Close()

	req, _ := http.NewRequest("POST", app.server.URL+path, body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawToken)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// The scopes column was written as "upload" at both creation sites and read
// nowhere, so it described nothing: an "upload" token created projects
// happily. It is checked now, and the backfill writes what each existing token
// could already do so that nobody's pipeline stops at the upgrade.
func TestTokenCreateScopeIsEnforced(t *testing.T) {
	app := setupTestApp(t)
	app.handler.config.Projects.AutoCreate = true
	ctx := context.Background()

	robot := &database.User{
		Username: "scope-bot", AuthSource: "robot", Role: "editor", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}

	uploadOnly, _ := auth.GenerateToken(32)
	if err := app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: robot.ID, TokenHash: auth.HashToken(uploadOnly), Name: "upload-only", Scopes: "upload",
	}); err != nil {
		t.Fatal(err)
	}
	mayCreate, _ := auth.GenerateToken(32)
	if err := app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: robot.ID, TokenHash: auth.HashToken(mayCreate), Name: "creator", Scopes: "upload,create",
	}); err != nil {
		t.Fatal(err)
	}

	resp := apiUpload(t, app, "/api/project/no-create/upload", uploadOnly, "", "v1.0.0")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 for an upload-only token, got %d", resp.StatusCode)
	}
	if _, err := app.handler.projects.GetBySlug(ctx, "no-create"); err == nil {
		t.Error("an upload-only token created a project")
	}

	resp2 := apiUpload(t, app, "/api/project/yes-create/upload", mayCreate, "", "v1.0.0")
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected a create-scoped token to still work, got %d", resp2.StatusCode)
	}

	// POST /api/projects reads the same scope.
	req, _ := http.NewRequest("POST", app.server.URL+"/api/projects",
		strings.NewReader(`{"slug":"explicit"}`))
	req.Header.Set("Authorization", "Bearer "+uploadOnly)
	direct, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer direct.Body.Close()
	if direct.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 from /api/projects for an upload-only token, got %d", direct.StatusCode)
	}
}

// An expiring token stops working when it says it will, and the pages offer
// the field that sets it — expires_at was honoured by the authenticator all
// along but nothing ever wrote it.
func TestTokenExpiryIsOfferedAndHonoured(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	project := seedProject(t, app, "docs", "Docs", false)

	robot := &database.User{
		Username: "exp-bot", AuthSource: "robot", Role: "editor", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}
	grantRole(t, app, project.ID, robot.ID, "editor")

	expired := time.Now().Add(-time.Hour)
	rawToken, _ := auth.GenerateToken(32)
	if err := app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: robot.ID, TokenHash: auth.HashToken(rawToken), Name: "old",
		Scopes: "upload", ExpiresAt: &expired,
	}); err != nil {
		t.Fatal(err)
	}

	resp := apiUpload(t, app, "/api/project/docs/upload", rawToken, "", "v1.0.0")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for an expired token, got %d", resp.StatusCode)
	}

	// Both pages offer the field, and the robots page sets it.
	for _, path := range []string{"/admin/robots", "/project/docs/tokens"} {
		if _, body := adminGet(t, app, cookies, path); !strings.Contains(body, `name="expires_in_days"`) {
			t.Errorf("%s: expected an expiry field on the token form", path)
		}
	}

	resp2 := adminPost(t, app, cookies, fmt.Sprintf("/admin/robots/%d/tokens", robot.ID),
		url.Values{"name": {"expiring"}, "expires_in_days": {"30"}})
	defer resp2.Body.Close()

	tokens, err := app.handler.tokens.ListByUser(ctx, robot.ID)
	if err != nil {
		t.Fatal(err)
	}
	var fresh *database.APIToken
	for i := range tokens {
		if tokens[i].Name == "expiring" {
			fresh = &tokens[i]
		}
	}
	if fresh == nil {
		t.Fatal("expected the token to be created")
	}
	if fresh.ExpiresAt == nil {
		t.Fatal("expected an expiry to be stored")
	}
	if days := time.Until(*fresh.ExpiresAt).Hours() / 24; days < 29 || days > 31 {
		t.Errorf("expected an expiry ~30 days out, got %.1f days", days)
	}
}

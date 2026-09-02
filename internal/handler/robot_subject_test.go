package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"

	"github.com/jmoiron/sqlx"
)

// A robot is an ordinary subject now (#155): the admin page creates it with
// the viewer role and a grant, and that grant is the whole of its reach. It
// used to be created as an instance editor, which meant every robot could
// upload to every project and only a token's scope ever narrowed it.
func TestRobotCreatedWithGrantNotInstanceEditor(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	mine := seedProject(t, app, "mine", "Mine", false)
	theirs := seedProject(t, app, "theirs", "Theirs", false)

	resp := adminPost(t, app, cookies, "/admin/robots", url.Values{
		"username": {"ci-bot"},
		"scope":    {fmt.Sprintf("project:%d", mine.ID)},
		"role":     {"editor"},
	})
	resp.Body.Close()

	robot, err := app.handler.users.GetByUsername(ctx, "ci-bot")
	if err != nil || robot == nil {
		t.Fatalf("expected the robot to exist: %v", err)
	}
	if robot.Role != "viewer" {
		t.Errorf("expected a robot to hold the viewer role, got %q", robot.Role)
	}
	if !robot.IsRobot {
		t.Error("expected the account to be marked as a robot")
	}

	// The grant, not the role, is what lets it upload — and only where granted.
	if !app.handler.resolver.CanUpload(ctx, robot, mine) {
		t.Error("expected the robot to upload to the project it was granted")
	}
	if app.handler.resolver.CanUpload(ctx, robot, theirs) {
		t.Error("expected the robot to be refused on a project it was not granted")
	}
}

// A robot with a token but no grant authenticates and then gets nowhere. That
// is the point: the credential says who you are, the grants say what you may
// do.
func TestRobotWithoutGrantsCannotUpload(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedProject(t, app, "docs", "Docs", false)
	robot := &database.User{
		Username: "no-grants", AuthSource: "robot", Role: "viewer", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}
	rawToken, _ := auth.GenerateToken(32)
	if err := app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: robot.ID, TokenHash: auth.HashToken(rawToken), Name: "t", Scopes: "upload",
	}); err != nil {
		t.Fatal(err)
	}

	resp := apiUpload(t, app, "/api/project/docs/upload", rawToken, "", "v1.0.0")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 403 for an ungranted robot, got %d: %s", resp.StatusCode, body)
	}
}

// A project's token belongs to a robot, not to whoever happened to click the
// button: the credential outlives the person, and the version history names
// the pipeline that pushed it.
func TestProjectTokenBelongsToARobot(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	admin := seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	project := seedProject(t, app, "docs", "Docs", false)

	form := url.Values{"name": {"ci"}, "csrf_token": {csrfTokenFor(t, app, cookies)}}
	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/tokens", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	tokens, err := app.handler.tokens.ListByProject(ctx, project.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("expected one token, got %d (%v)", len(tokens), err)
	}
	if tokens[0].UserID == admin.ID {
		t.Fatal("the token still speaks for the person who created it")
	}
	if tokens[0].ProjectID == nil || *tokens[0].ProjectID != project.ID {
		t.Error("expected the token to be scoped to this project")
	}

	robot, err := app.handler.users.GetByUsername(ctx, "docs-bot")
	if err != nil || robot == nil {
		t.Fatalf("expected a robot named docs-bot: %v", err)
	}
	if robot.ID != tokens[0].UserID {
		t.Error("expected the token to belong to that robot")
	}
	if robot.Role != "viewer" {
		t.Errorf("expected the robot to hold the viewer role, got %q", robot.Role)
	}
	if !app.handler.resolver.CanUpload(ctx, robot, project) {
		t.Error("expected the robot to be granted editor on the project")
	}

	// A second token reuses the robot rather than inventing another one, and
	// does not pile up a second grant.
	form.Set("name", "release")
	req2, _ := http.NewRequest("POST", app.server.URL+"/project/docs/tokens", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req2.AddCookie(c)
	}
	resp2, err := (&http.Client{}).Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	robots, err := app.handler.users.ListRobots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(robots) != 1 {
		t.Errorf("expected one robot, got %d", len(robots))
	}
	grants, err := app.handler.accessGrants.ListByUser(ctx, robot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 {
		t.Errorf("expected one grant, got %d", len(grants))
	}
}

// Naming a person in the robot field would hand their account a bearer
// credential, which is the thing this whole issue is about.
func TestProjectTokenRefusesAPersonsAccount(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")
	project := seedProject(t, app, "docs", "Docs", false)

	form := url.Values{
		"name":       {"ci"},
		"robot":      {"admin"},
		"csrf_token": {csrfTokenFor(t, app, cookies)},
	}
	req, _ := http.NewRequest("POST", app.server.URL+"/project/docs/tokens", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "is a person&#39;s account") &&
		!strings.Contains(string(body), "is a person's account") {
		t.Errorf("expected the page to refuse a person's account, got: %s", body)
	}
	tokens, _ := app.handler.tokens.ListByProject(ctx, project.ID)
	if len(tokens) != 0 {
		t.Errorf("expected no token to be issued, got %d", len(tokens))
	}
}

// The one-shot migration must preserve reach exactly: a robot that could
// upload everywhere still can, through grants this time, so nobody's CI stops
// working on the upgrade.
func TestMigrateRobotSubjectsPreservesReach(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	project := seedProject(t, app, "docs", "Docs", false)
	robot := &database.User{
		Username: "old-bot", AuthSource: "robot", Role: "editor", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}
	// A robot an operator promoted deliberately is left alone.
	adminBot := &database.User{
		Username: "admin-bot", AuthSource: "robot", Role: "admin", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, adminBot); err != nil {
		t.Fatal(err)
	}

	db := app.db.(*sqlx.DB)
	if err := sqlstore.MigrateRobotSubjects(ctx, db, nil); err != nil {
		t.Fatal(err)
	}

	migrated, err := app.handler.users.GetByUsername(ctx, "old-bot")
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Role != "viewer" {
		t.Errorf("expected the robot to be demoted to viewer, got %q", migrated.Role)
	}
	if !app.handler.resolver.CanUpload(ctx, migrated, project) {
		t.Error("expected the migrated robot to still upload where it could before")
	}

	untouched, err := app.handler.users.GetByUsername(ctx, "admin-bot")
	if err != nil {
		t.Fatal(err)
	}
	if untouched.Role != "admin" {
		t.Errorf("expected an admin robot to be left alone, got %q", untouched.Role)
	}

	// Running twice must not double the grants: the marker is the guard.
	if err := sqlstore.MigrateRobotSubjects(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	grants, err := app.handler.accessGrants.ListByUser(ctx, migrated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 {
		t.Errorf("expected one org grant, got %d", len(grants))
	}
}

// Auto-create is project creation, so it needs somewhere to put the project.
// An org-level editor grant is what "may add projects here" means.
func TestAutoCreateUsesTheOrgTheRobotWasGranted(t *testing.T) {
	app := setupTestApp(t)
	app.handler.config.Projects.AutoCreate = true
	ctx := context.Background()

	robot := &database.User{
		Username: "org-bot", AuthSource: "robot", Role: "viewer", IsRobot: true,
	}
	if err := app.handler.users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}
	rawToken, _ := auth.GenerateToken(32)
	if err := app.handler.tokens.Create(ctx, &database.APIToken{
		UserID: robot.ID, TokenHash: auth.HashToken(rawToken), Name: "t", Scopes: "upload,create",
	}); err != nil {
		t.Fatal(err)
	}

	// No grant: nowhere to create, so no project.
	resp := apiUpload(t, app, "/api/project/nope/upload", rawToken, "", "v1.0.0")
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 without a grant, got %d", resp.StatusCode)
	}

	orgID := defaultOrgID(t, app)
	grantOrgRole(t, app, orgID, robot.ID, "editor")

	resp2 := apiUpload(t, app, "/api/project/yes/upload", rawToken, "", "v1.0.0")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("expected the granted robot to create in its org, got %d: %s", resp2.StatusCode, body)
	}
	project, err := app.handler.projects.GetBySlug(ctx, "yes")
	if err != nil {
		t.Fatal(err)
	}
	if project.OrgID == nil || *project.OrgID != orgID {
		t.Errorf("expected the project in org %d, got %v", orgID, project.OrgID)
	}

	// Granted on two organizations, "which one" has no honest answer.
	second := &database.Org{Slug: "second", Name: "Second"}
	if err := app.handler.orgs.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	grantOrgRole(t, app, second.ID, robot.ID, "editor")

	resp3 := apiUpload(t, app, "/api/project/ambiguous/upload", rawToken, "", "v1.0.0")
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 when several orgs qualify, got %d", resp3.StatusCode)
	}
}

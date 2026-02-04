package sql

import (
	"context"
	"testing"
	"time"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/testutil"
)

func TestProjectStoreCRUD(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := NewProjectStore(db)
	ctx := context.Background()

	// Create
	project := &database.Project{
		Slug:        "test-project",
		Name:        "Test Project",
		Description: "A test project",
		Visibility:  database.VisibilityPublic,
	}
	if err := store.Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	if project.ID == 0 {
		t.Error("expected non-zero ID after create")
	}

	// GetBySlug
	got, err := store.GetBySlug(ctx, "test-project")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Test Project" {
		t.Errorf("expected name 'Test Project', got %q", got.Name)
	}

	// GetByID
	got2, err := store.GetByID(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Slug != "test-project" {
		t.Errorf("expected slug 'test-project', got %q", got2.Slug)
	}

	// List
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 project, got %d", len(list))
	}

	// ListByVisibility
	public, err := store.ListByVisibility(ctx, database.VisibilityPublic)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 1 {
		t.Errorf("expected 1 public project, got %d", len(public))
	}

	// Search
	results, err := store.Search(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 search result, got %d", len(results))
	}

	results, err = store.Search(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 search results, got %d", len(results))
	}

	// Update
	project.Name = "Updated Project"
	project.Visibility = database.VisibilityCustom
	if err := store.Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	got3, _ := store.GetByID(ctx, project.ID)
	if got3.Name != "Updated Project" {
		t.Errorf("expected updated name, got %q", got3.Name)
	}
	if got3.Visibility != database.VisibilityCustom {
		t.Errorf("expected visibility 'custom', got %q", got3.Visibility)
	}

	// ListByVisibility should now return 0 public projects
	public, _ = store.ListByVisibility(ctx, database.VisibilityPublic)
	if len(public) != 0 {
		t.Errorf("expected 0 public projects, got %d", len(public))
	}

	// Delete
	if err := store.Delete(ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = store.List(ctx)
	if len(list) != 0 {
		t.Errorf("expected 0 projects after delete, got %d", len(list))
	}
}

func TestVersionStoreCRUD(t *testing.T) {
	db := testutil.NewTestDB(t)
	pStore := NewProjectStore(db)
	vStore := NewVersionStore(db)
	uStore := NewUserStore(db)
	ctx := context.Background()

	// Create a user first (needed for uploaded_by FK)
	pwd := "test"
	user := &database.User{
		Username:   "testuser",
		Password:   &pwd,
		AuthSource: "builtin",
		Role:       "admin",
	}
	if err := uStore.Create(ctx, user); err != nil {
		t.Fatal(err)
	}

	// Create a project
	project := &database.Project{
		Slug: "proj",
		Name: "Proj",
	}
	if err := pStore.Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	// Create version
	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v1.0.0",
		StoragePath: "/data/proj/v1.0.0",
		UploadedBy:  user.ID,
	}
	if err := vStore.Create(ctx, version); err != nil {
		t.Fatal(err)
	}
	if version.ID == 0 {
		t.Error("expected non-zero ID after create")
	}

	// GetByProjectAndTag
	got, err := vStore.GetByProjectAndTag(ctx, project.ID, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.StoragePath != "/data/proj/v1.0.0" {
		t.Errorf("expected storage path, got %q", got.StoragePath)
	}

	// ListByProject
	list, err := vStore.ListByProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 version, got %d", len(list))
	}

	// Create a second version
	v2 := &database.Version{
		ProjectID:   project.ID,
		Tag:         "v2.0.0",
		StoragePath: "/data/proj/v2.0.0",
		UploadedBy:  user.ID,
	}
	if err := vStore.Create(ctx, v2); err != nil {
		t.Fatal(err)
	}

	list, _ = vStore.ListByProject(ctx, project.ID)
	if len(list) != 2 {
		t.Errorf("expected 2 versions, got %d", len(list))
	}

	// Delete
	if err := vStore.Delete(ctx, version.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = vStore.ListByProject(ctx, project.ID)
	if len(list) != 1 {
		t.Errorf("expected 1 version after delete, got %d", len(list))
	}
}

func TestUserStoreCRUD(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := NewUserStore(db)
	ctx := context.Background()

	// Create
	pwd := "hashedpw"
	user := &database.User{
		Username:   "alice",
		Email:      "alice@example.com",
		Password:   &pwd,
		AuthSource: "builtin",
		Role:       "admin",
	}
	if err := store.Create(ctx, user); err != nil {
		t.Fatal(err)
	}

	// GetByUsername
	got, err := store.GetByUsername(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("expected email, got %q", got.Email)
	}

	// Count
	count, err := store.Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected count 1, got %d", count)
	}

	// ListRobots (should be empty)
	robots, err := store.ListRobots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(robots) != 0 {
		t.Errorf("expected 0 robots, got %d", len(robots))
	}

	// Create robot
	robot := &database.User{
		Username:   "ci-bot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	if err := store.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}

	robots, _ = store.ListRobots(ctx)
	if len(robots) != 1 {
		t.Errorf("expected 1 robot, got %d", len(robots))
	}

	// List (non-robots only)
	users, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Errorf("expected 1 non-robot user, got %d", len(users))
	}

	// Delete
	if err := store.Delete(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	count, _ = store.Count(ctx)
	if count != 1 {
		t.Errorf("expected count 1 after delete, got %d", count)
	}
}

func TestSessionStoreCRUD(t *testing.T) {
	db := testutil.NewTestDB(t)
	sStore := NewSessionStore(db)
	uStore := NewUserStore(db)
	ctx := context.Background()

	// Create user
	pwd := "test"
	user := &database.User{
		Username:   "testuser",
		Password:   &pwd,
		AuthSource: "builtin",
		Role:       "viewer",
	}
	if err := uStore.Create(ctx, user); err != nil {
		t.Fatal(err)
	}

	// Create session
	session := &database.Session{
		ID:        "test-session-token",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := sStore.Create(ctx, session); err != nil {
		t.Fatal(err)
	}

	// GetByID
	got, err := sStore.GetByID(ctx, "test-session-token")
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != user.ID {
		t.Errorf("expected user_id %d, got %d", user.ID, got.UserID)
	}

	// Delete
	if err := sStore.Delete(ctx, "test-session-token"); err != nil {
		t.Fatal(err)
	}
	_, err = sStore.GetByID(ctx, "test-session-token")
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestSessionStoreDeleteExpired(t *testing.T) {
	db := testutil.NewTestDB(t)
	sStore := NewSessionStore(db)
	uStore := NewUserStore(db)
	ctx := context.Background()

	pwd := "test"
	user := &database.User{
		Username: "testuser", Password: &pwd,
		AuthSource: "builtin", Role: "viewer",
	}
	uStore.Create(ctx, user)

	// Create an expired session
	expired := &database.Session{
		ID:        "expired-token",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	sStore.Create(ctx, expired)

	// Create a valid session
	valid := &database.Session{
		ID:        "valid-token",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	sStore.Create(ctx, valid)

	// Delete expired
	if err := sStore.DeleteExpired(ctx); err != nil {
		t.Fatal(err)
	}

	// Expired should be gone
	_, err := sStore.GetByID(ctx, "expired-token")
	if err == nil {
		t.Error("expected expired session to be deleted")
	}

	// Valid should remain
	_, err = sStore.GetByID(ctx, "valid-token")
	if err != nil {
		t.Error("expected valid session to still exist")
	}
}

func TestProjectAccessStore(t *testing.T) {
	db := testutil.NewTestDB(t)
	aStore := NewProjectAccessStore(db)
	pStore := NewProjectStore(db)
	uStore := NewUserStore(db)
	ctx := context.Background()

	// Setup
	pwd := "test"
	user := &database.User{Username: "testuser", Password: &pwd, AuthSource: "builtin", Role: "viewer"}
	uStore.Create(ctx, user)
	project := &database.Project{Slug: "test-proj", Name: "Test"}
	pStore.Create(ctx, project)

	// Grant
	access := &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    user.ID,
		Role:      "editor",
	}
	if err := aStore.Grant(ctx, access); err != nil {
		t.Fatal(err)
	}

	// GetAccess
	got, err := aStore.GetAccess(ctx, project.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "editor" {
		t.Errorf("expected role editor, got %q", got.Role)
	}

	// ListAccessibleProjectIDs
	ids, err := aStore.ListAccessibleProjectIDs(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != project.ID {
		t.Errorf("expected [%d], got %v", project.ID, ids)
	}

	// Revoke
	if err := aStore.Revoke(ctx, project.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	_, err = aStore.GetAccess(ctx, project.ID, user.ID)
	if err == nil {
		t.Error("expected error after revoke")
	}
}

func TestTokenStoreCRUD(t *testing.T) {
	db := testutil.NewTestDB(t)
	tStore := NewTokenStore(db)
	uStore := NewUserStore(db)
	ctx := context.Background()

	// Create user
	user := &database.User{Username: "robot", AuthSource: "robot", Role: "editor", IsRobot: true}
	uStore.Create(ctx, user)

	// Create token
	token := &database.APIToken{
		UserID:    user.ID,
		TokenHash: "abc123hash",
		Name:      "ci-token",
		Scopes:    "upload",
	}
	if err := tStore.Create(ctx, token); err != nil {
		t.Fatal(err)
	}

	// GetByHash
	got, err := tStore.GetByHash(ctx, "abc123hash")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "ci-token" {
		t.Errorf("expected name ci-token, got %q", got.Name)
	}

	// GetByID
	gotByID, err := tStore.GetByID(ctx, token.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotByID.Name != "ci-token" {
		t.Errorf("expected name ci-token, got %q", gotByID.Name)
	}

	// ListByUser
	tokens, err := tStore.ListByUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(tokens))
	}

	// Delete
	if err := tStore.Delete(ctx, token.ID); err != nil {
		t.Fatal(err)
	}
	_, err = tStore.GetByHash(ctx, "abc123hash")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestTokenStoreProjectScoped(t *testing.T) {
	db := testutil.NewTestDB(t)
	tStore := NewTokenStore(db)
	uStore := NewUserStore(db)
	pStore := NewProjectStore(db)
	ctx := context.Background()

	// Create user
	user := &database.User{Username: "robot", AuthSource: "robot", Role: "editor", IsRobot: true}
	uStore.Create(ctx, user)

	// Create projects
	project1 := &database.Project{Slug: "proj1", Name: "Project 1", Visibility: database.VisibilityPublic}
	pStore.Create(ctx, project1)
	project2 := &database.Project{Slug: "proj2", Name: "Project 2", Visibility: database.VisibilityPublic}
	pStore.Create(ctx, project2)

	// Create global token (no project_id)
	globalToken := &database.APIToken{
		UserID:    user.ID,
		ProjectID: nil,
		TokenHash: "global-hash",
		Name:      "global-token",
		Scopes:    "upload",
	}
	if err := tStore.Create(ctx, globalToken); err != nil {
		t.Fatal(err)
	}

	// Create project-scoped token for project1
	scopedToken1 := &database.APIToken{
		UserID:    user.ID,
		ProjectID: &project1.ID,
		TokenHash: "scoped1-hash",
		Name:      "scoped-token-1",
		Scopes:    "upload",
	}
	if err := tStore.Create(ctx, scopedToken1); err != nil {
		t.Fatal(err)
	}

	// Create another project-scoped token for project1
	scopedToken2 := &database.APIToken{
		UserID:    user.ID,
		ProjectID: &project1.ID,
		TokenHash: "scoped2-hash",
		Name:      "scoped-token-2",
		Scopes:    "upload",
	}
	if err := tStore.Create(ctx, scopedToken2); err != nil {
		t.Fatal(err)
	}

	// Create project-scoped token for project2
	scopedToken3 := &database.APIToken{
		UserID:    user.ID,
		ProjectID: &project2.ID,
		TokenHash: "scoped3-hash",
		Name:      "scoped-token-3",
		Scopes:    "upload",
	}
	if err := tStore.Create(ctx, scopedToken3); err != nil {
		t.Fatal(err)
	}

	// ListByProject for project1 should return 2 tokens
	proj1Tokens, err := tStore.ListByProject(ctx, project1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj1Tokens) != 2 {
		t.Errorf("expected 2 tokens for project1, got %d", len(proj1Tokens))
	}

	// ListByProject for project2 should return 1 token
	proj2Tokens, err := tStore.ListByProject(ctx, project2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj2Tokens) != 1 {
		t.Errorf("expected 1 token for project2, got %d", len(proj2Tokens))
	}

	// Verify global token has nil ProjectID
	gotGlobal, _ := tStore.GetByHash(ctx, "global-hash")
	if gotGlobal.ProjectID != nil {
		t.Error("expected nil ProjectID for global token")
	}

	// Verify scoped token has correct ProjectID
	gotScoped, _ := tStore.GetByHash(ctx, "scoped1-hash")
	if gotScoped.ProjectID == nil {
		t.Fatal("expected non-nil ProjectID for scoped token")
	}
	if *gotScoped.ProjectID != project1.ID {
		t.Errorf("expected ProjectID %d, got %d", project1.ID, *gotScoped.ProjectID)
	}

	// GetByID should return ProjectID
	gotByID, _ := tStore.GetByID(ctx, scopedToken1.ID)
	if gotByID.ProjectID == nil || *gotByID.ProjectID != project1.ID {
		t.Error("GetByID should return correct ProjectID")
	}
}

func TestTokenStoreGetByIDNotFound(t *testing.T) {
	db := testutil.NewTestDB(t)
	tStore := NewTokenStore(db)
	ctx := context.Background()

	_, err := tStore.GetByID(ctx, 99999)
	if err == nil {
		t.Error("expected error for non-existent token ID")
	}
}

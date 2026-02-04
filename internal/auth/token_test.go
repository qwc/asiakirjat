package auth

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qwc/asiakirjat/internal/database"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
)

func setupTokenAuth(t *testing.T) (*TokenAuthenticator, *sqlstore.TokenStore, *sqlstore.UserStore, *sqlstore.ProjectStore) {
	t.Helper()
	db := testutil.NewTestDB(t)
	tokenStore := sqlstore.NewTokenStore(db)
	userStore := sqlstore.NewUserStore(db)
	projectStore := sqlstore.NewProjectStore(db)
	auth := NewTokenAuthenticator(tokenStore, userStore)
	return auth, tokenStore, userStore, projectStore
}

func TestTokenAuthenticateRequestSuccess(t *testing.T) {
	auth, tokenStore, userStore, _ := setupTokenAuth(t)
	ctx := context.Background()

	// Create user
	user := &database.User{
		Username:   "robot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	userStore.Create(ctx, user)

	// Create token
	rawToken := "test-token-12345"
	tokenHash := HashToken(rawToken)
	tokenStore.Create(ctx, &database.APIToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		Name:      "test-token",
		Scopes:    "upload",
	})

	// Create request with valid token
	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	got := auth.AuthenticateRequest(req)
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.Username != "robot" {
		t.Errorf("expected username 'robot', got %q", got.Username)
	}
}

func TestTokenAuthenticateRequestNoHeader(t *testing.T) {
	auth, _, _, _ := setupTokenAuth(t)

	req := httptest.NewRequest("POST", "/api/upload", nil)
	// No Authorization header

	got := auth.AuthenticateRequest(req)
	if got != nil {
		t.Error("expected nil for missing auth header")
	}
}

func TestTokenAuthenticateRequestInvalidFormat(t *testing.T) {
	auth, _, _, _ := setupTokenAuth(t)

	testCases := []struct {
		name   string
		header string
	}{
		{"Basic auth", "Basic dXNlcjpwYXNz"},
		{"No space", "Bearertoken123"},
		{"Empty bearer", "Bearer "},
		{"Just Bearer", "Bearer"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/upload", nil)
			req.Header.Set("Authorization", tc.header)

			got := auth.AuthenticateRequest(req)
			if got != nil {
				t.Errorf("expected nil for invalid header format: %s", tc.header)
			}
		})
	}
}

func TestTokenAuthenticateRequestInvalidToken(t *testing.T) {
	auth, _, _, _ := setupTokenAuth(t)

	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer nonexistent-token")

	got := auth.AuthenticateRequest(req)
	if got != nil {
		t.Error("expected nil for invalid token")
	}
}

func TestTokenAuthenticateRequestExpiredToken(t *testing.T) {
	auth, tokenStore, userStore, _ := setupTokenAuth(t)
	ctx := context.Background()

	// Create user
	user := &database.User{
		Username:   "robot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	userStore.Create(ctx, user)

	// Create expired token
	rawToken := "expired-token-12345"
	tokenHash := HashToken(rawToken)
	expiredAt := time.Now().Add(-1 * time.Hour)
	tokenStore.Create(ctx, &database.APIToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		Name:      "expired-token",
		Scopes:    "upload",
		ExpiresAt: &expiredAt,
	})

	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	got := auth.AuthenticateRequest(req)
	if got != nil {
		t.Error("expected nil for expired token")
	}
}

func TestTokenAuthenticateRequestUserDeleted(t *testing.T) {
	auth, tokenStore, userStore, _ := setupTokenAuth(t)
	ctx := context.Background()

	// Create user
	user := &database.User{
		Username:   "robot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	userStore.Create(ctx, user)

	// Create token
	rawToken := "orphan-token-12345"
	tokenHash := HashToken(rawToken)
	tokenStore.Create(ctx, &database.APIToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		Name:      "orphan-token",
		Scopes:    "upload",
	})

	// Delete user
	userStore.Delete(ctx, user.ID)

	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	got := auth.AuthenticateRequest(req)
	if got != nil {
		t.Error("expected nil for token with deleted user")
	}
}

func TestTokenAuthenticateRequestForProjectGlobalToken(t *testing.T) {
	auth, tokenStore, userStore, projectStore := setupTokenAuth(t)
	ctx := context.Background()

	// Create user
	user := &database.User{
		Username:   "robot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	userStore.Create(ctx, user)

	// Create project
	project := &database.Project{
		Slug:     "test-proj",
		Name:     "Test Project",
		Visibility: database.VisibilityPublic,
	}
	projectStore.Create(ctx, project)

	// Create global token (no project_id)
	rawToken := "global-token-12345"
	tokenHash := HashToken(rawToken)
	tokenStore.Create(ctx, &database.APIToken{
		UserID:    user.ID,
		ProjectID: nil, // Global token
		TokenHash: tokenHash,
		Name:      "global-token",
		Scopes:    "upload",
	})

	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	// Global token should work for any project
	got := auth.AuthenticateRequestForProject(req, project.ID)
	if got == nil {
		t.Fatal("expected user for global token")
	}
	if got.Username != "robot" {
		t.Errorf("expected username 'robot', got %q", got.Username)
	}
}

func TestTokenAuthenticateRequestForProjectScopedTokenCorrectProject(t *testing.T) {
	auth, tokenStore, userStore, projectStore := setupTokenAuth(t)
	ctx := context.Background()

	// Create user
	user := &database.User{
		Username:   "robot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	userStore.Create(ctx, user)

	// Create project
	project := &database.Project{
		Slug:     "test-proj",
		Name:     "Test Project",
		Visibility: database.VisibilityPublic,
	}
	projectStore.Create(ctx, project)

	// Create project-scoped token
	rawToken := "scoped-token-12345"
	tokenHash := HashToken(rawToken)
	tokenStore.Create(ctx, &database.APIToken{
		UserID:    user.ID,
		ProjectID: &project.ID,
		TokenHash: tokenHash,
		Name:      "scoped-token",
		Scopes:    "upload",
	})

	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	// Scoped token should work for correct project
	got := auth.AuthenticateRequestForProject(req, project.ID)
	if got == nil {
		t.Fatal("expected user for scoped token on correct project")
	}
	if got.Username != "robot" {
		t.Errorf("expected username 'robot', got %q", got.Username)
	}
}

func TestTokenAuthenticateRequestForProjectScopedTokenWrongProject(t *testing.T) {
	auth, tokenStore, userStore, projectStore := setupTokenAuth(t)
	ctx := context.Background()

	// Create user
	user := &database.User{
		Username:   "robot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	userStore.Create(ctx, user)

	// Create two projects
	project1 := &database.Project{Slug: "proj1", Name: "Project 1", Visibility: database.VisibilityPublic}
	projectStore.Create(ctx, project1)
	project2 := &database.Project{Slug: "proj2", Name: "Project 2", Visibility: database.VisibilityPublic}
	projectStore.Create(ctx, project2)

	// Create token scoped to project1
	rawToken := "scoped-token-12345"
	tokenHash := HashToken(rawToken)
	tokenStore.Create(ctx, &database.APIToken{
		UserID:    user.ID,
		ProjectID: &project1.ID,
		TokenHash: tokenHash,
		Name:      "scoped-token",
		Scopes:    "upload",
	})

	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	// Scoped token should NOT work for different project
	got := auth.AuthenticateRequestForProject(req, project2.ID)
	if got != nil {
		t.Error("expected nil for scoped token on wrong project")
	}
}

func TestTokenAuthenticateRequestForProjectExpiredToken(t *testing.T) {
	auth, tokenStore, userStore, projectStore := setupTokenAuth(t)
	ctx := context.Background()

	// Create user
	user := &database.User{
		Username:   "robot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	userStore.Create(ctx, user)

	// Create project
	project := &database.Project{Slug: "proj", Name: "Project", Visibility: database.VisibilityPublic}
	projectStore.Create(ctx, project)

	// Create expired global token
	rawToken := "expired-global-token"
	tokenHash := HashToken(rawToken)
	expiredAt := time.Now().Add(-1 * time.Hour)
	tokenStore.Create(ctx, &database.APIToken{
		UserID:    user.ID,
		ProjectID: nil,
		TokenHash: tokenHash,
		Name:      "expired-token",
		Scopes:    "upload",
		ExpiresAt: &expiredAt,
	})

	req := httptest.NewRequest("POST", "/api/upload", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	got := auth.AuthenticateRequestForProject(req, project.ID)
	if got != nil {
		t.Error("expected nil for expired token")
	}
}

func TestTokenAuthenticateRequestCaseInsensitiveBearer(t *testing.T) {
	auth, tokenStore, userStore, _ := setupTokenAuth(t)
	ctx := context.Background()

	// Create user
	user := &database.User{
		Username:   "robot",
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}
	userStore.Create(ctx, user)

	// Create token
	rawToken := "test-token-12345"
	tokenHash := HashToken(rawToken)
	tokenStore.Create(ctx, &database.APIToken{
		UserID:    user.ID,
		TokenHash: tokenHash,
		Name:      "test-token",
		Scopes:    "upload",
	})

	testCases := []string{"Bearer", "bearer", "BEARER", "BeArEr"}

	for _, bearer := range testCases {
		t.Run(bearer, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/upload", nil)
			req.Header.Set("Authorization", bearer+" "+rawToken)

			got := auth.AuthenticateRequest(req)
			if got == nil {
				t.Errorf("expected user for '%s' prefix", bearer)
			}
		})
	}
}

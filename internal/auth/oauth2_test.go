package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
	"golang.org/x/oauth2"
)

func TestOAuth2AuthenticatorName(t *testing.T) {
	auth := NewOAuth2Authenticator(config.OAuth2Config{}, nil, nil)
	if auth.Name() != "oauth2" {
		t.Errorf("expected name 'oauth2', got %q", auth.Name())
	}
}

func TestOAuth2DirectAuthFails(t *testing.T) {
	auth := NewOAuth2Authenticator(config.OAuth2Config{}, nil, nil)
	_, err := auth.Authenticate(context.Background(), "user", "pass")
	if err == nil {
		t.Error("expected error for direct OAuth2 authentication")
	}
}

func TestOAuth2StateGeneration(t *testing.T) {
	auth := NewOAuth2Authenticator(config.OAuth2Config{
		AuthURL:  "http://localhost/auth",
		TokenURL: "http://localhost/token",
	}, nil, nil)

	url, err := auth.GenerateAuthURL()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(url, "http://localhost/auth") {
		t.Error("expected auth URL in generated URL")
	}
	if !strings.Contains(url, "state=") {
		t.Error("expected state parameter in URL")
	}
}

func TestOAuth2StateValidation(t *testing.T) {
	auth := NewOAuth2Authenticator(config.OAuth2Config{
		AuthURL:  "http://localhost/auth",
		TokenURL: "http://localhost/token",
	}, nil, nil)

	// Generate a state
	url, _ := auth.GenerateAuthURL()

	// Extract state from URL
	parts := strings.Split(url, "state=")
	if len(parts) < 2 {
		t.Fatal("no state in URL")
	}
	state := strings.Split(parts[1], "&")[0]

	// Valid state should be consumed
	if !auth.ValidateState(state) {
		t.Error("expected state to be valid")
	}

	// Same state should not be valid again (consumed)
	if auth.ValidateState(state) {
		t.Error("expected state to be consumed")
	}

	// Unknown state should not be valid
	if auth.ValidateState("unknown-state") {
		t.Error("expected unknown state to be invalid")
	}
}

func TestOAuth2HandleCallback(t *testing.T) {
	// Set up mock OAuth2 provider
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "mock-access-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"refresh_token": "mock-refresh-token",
		})
	}))
	defer tokenServer.Close()

	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Bearer token is present
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sub":                "12345",
			"preferred_username": "oauth-user",
			"email":              "oauth@example.com",
			"name":               "OAuth User",
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		AuthURL:      tokenServer.URL + "/auth",
		TokenURL:     tokenServer.URL,
		UserInfoURL:  userInfoServer.URL,
		RedirectURL:  "http://localhost/callback",
	}, userStore, logger)

	// Override the oauth config to use test server
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			AuthURL:  tokenServer.URL + "/auth",
			TokenURL: tokenServer.URL,
		},
		RedirectURL: "http://localhost/callback",
	}

	ctx := context.Background()
	user, err := auth.HandleCallback(ctx, "mock-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.Username != "oauth-user" {
		t.Errorf("expected username 'oauth-user', got %q", user.Username)
	}
	if user.Email != "oauth@example.com" {
		t.Errorf("expected email 'oauth@example.com', got %q", user.Email)
	}
	if user.AuthSource != "oauth2" {
		t.Errorf("expected auth source 'oauth2', got %q", user.AuthSource)
	}
	if user.Role != "viewer" {
		t.Errorf("expected default role 'viewer', got %q", user.Role)
	}
}

func TestOAuth2HandleCallbackExistingUser(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"preferred_username": "existing-user",
			"email":              "new@example.com",
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	// Pre-create the user
	ctx := context.Background()
	existing := &database.User{
		Username:   "existing-user",
		Email:      "old@example.com",
		AuthSource: "oauth2",
		Role:       "editor",
	}
	userStore.Create(ctx, existing)

	auth := NewOAuth2Authenticator(config.OAuth2Config{}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	user, err := auth.HandleCallback(ctx, "mock-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return existing user with updated email
	if user.ID != existing.ID {
		t.Error("expected to return existing user")
	}
	// Role should be updated to viewer (no groups configured means viewer)
	if user.Role != "viewer" {
		t.Errorf("expected role 'viewer', got %q", user.Role)
	}
}

func TestOAuth2HandleCallbackEmailOnly(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"email": "alice@example.com",
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	ctx := context.Background()
	user, err := auth.HandleCallback(ctx, "mock-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Username should be derived from email
	if user.Username != "alice" {
		t.Errorf("expected username 'alice' derived from email, got %q", user.Username)
	}
}

func TestOAuth2HandleCallbackWithGroups(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"preferred_username": "group-user",
			"email":              "group@example.com",
			"groups":             []string{"asiakirjat-admins", "other-group"},
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{
		GroupsClaim: "groups",
		AdminGroup:  "asiakirjat-admins",
		EditorGroup: "asiakirjat-editors",
	}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	ctx := context.Background()
	user, err := auth.HandleCallback(ctx, "mock-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// User should be admin based on group membership
	if user.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", user.Role)
	}
}

func TestOAuth2HandleCallbackWithViewerGroup(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	// User not in any allowed group
	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"preferred_username": "denied-user",
			"email":              "denied@example.com",
			"groups":             []string{"other-group"},
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{
		GroupsClaim: "groups",
		AdminGroup:  "asiakirjat-admins",
		EditorGroup: "asiakirjat-editors",
		ViewerGroup: "asiakirjat-viewers", // When set, requires membership
	}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	ctx := context.Background()
	_, err := auth.HandleCallback(ctx, "mock-code")
	if err == nil {
		t.Error("expected error for user not in any allowed group")
	}
	if !strings.Contains(err.Error(), "not in any allowed group") {
		t.Errorf("expected 'not in any allowed group' error, got: %v", err)
	}
}

func TestExtractGroups(t *testing.T) {
	tests := []struct {
		name      string
		rawInfo   map[string]any
		claimName string
		expected  []string
	}{
		{
			name: "simple groups array",
			rawInfo: map[string]any{
				"groups": []any{"admin", "editor"},
			},
			claimName: "groups",
			expected:  []string{"admin", "editor"},
		},
		{
			name: "nested claim",
			rawInfo: map[string]any{
				"realm_access": map[string]any{
					"roles": []any{"admin", "user"},
				},
			},
			claimName: "realm_access.roles",
			expected:  []string{"admin", "user"},
		},
		{
			name: "cognito style",
			rawInfo: map[string]any{
				"cognito:groups": []any{"admins", "readers"},
			},
			claimName: "cognito:groups",
			expected:  []string{"admins", "readers"},
		},
		{
			name: "missing claim",
			rawInfo: map[string]any{
				"email": "test@example.com",
			},
			claimName: "groups",
			expected:  nil,
		},
		{
			name: "empty claim",
			rawInfo: map[string]any{
				"groups": []any{},
			},
			claimName: "groups",
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGroups(tt.rawInfo, tt.claimName)
			if len(got) != len(tt.expected) {
				t.Errorf("expected %d groups, got %d", len(tt.expected), len(got))
				return
			}
			for i, g := range got {
				if g != tt.expected[i] {
					t.Errorf("expected group %d to be %q, got %q", i, tt.expected[i], g)
				}
			}
		})
	}
}

func TestMapGroupsToRole(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.OAuth2Config
		groups      []string
		expectedRole string
		expectedAllowed bool
	}{
		{
			name:        "no group config - allow as viewer",
			cfg:         config.OAuth2Config{},
			groups:      []string{"random-group"},
			expectedRole: "viewer",
			expectedAllowed: true,
		},
		{
			name: "admin group member",
			cfg: config.OAuth2Config{
				AdminGroup:  "admins",
				EditorGroup: "editors",
			},
			groups:      []string{"admins", "users"},
			expectedRole: "admin",
			expectedAllowed: true,
		},
		{
			name: "editor group member",
			cfg: config.OAuth2Config{
				AdminGroup:  "admins",
				EditorGroup: "editors",
			},
			groups:      []string{"editors", "users"},
			expectedRole: "editor",
			expectedAllowed: true,
		},
		{
			name: "admin takes priority over editor",
			cfg: config.OAuth2Config{
				AdminGroup:  "admins",
				EditorGroup: "editors",
			},
			groups:      []string{"editors", "admins"},
			expectedRole: "admin",
			expectedAllowed: true,
		},
		{
			name: "viewer group when viewerGroup set",
			cfg: config.OAuth2Config{
				AdminGroup:  "admins",
				EditorGroup: "editors",
				ViewerGroup: "viewers",
			},
			groups:      []string{"viewers"},
			expectedRole: "viewer",
			expectedAllowed: true,
		},
		{
			name: "not in any group when viewerGroup set - denied",
			cfg: config.OAuth2Config{
				AdminGroup:  "admins",
				EditorGroup: "editors",
				ViewerGroup: "viewers",
			},
			groups:      []string{"random-group"},
			expectedRole: "",
			expectedAllowed: false,
		},
		{
			name: "case insensitive match",
			cfg: config.OAuth2Config{
				AdminGroup: "Admins",
			},
			groups:      []string{"ADMINS"},
			expectedRole: "admin",
			expectedAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := NewOAuth2Authenticator(tt.cfg, nil, nil)
			role, allowed := auth.mapGroupsToRole(tt.groups)
			if role != tt.expectedRole {
				t.Errorf("expected role %q, got %q", tt.expectedRole, role)
			}
			if allowed != tt.expectedAllowed {
				t.Errorf("expected allowed=%v, got allowed=%v", tt.expectedAllowed, allowed)
			}
		})
	}
}

func TestValidateOAuth2Config(t *testing.T) {
	valid := config.OAuth2Config{
		Enabled:      true,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		AuthURL:      "https://auth.example.com/authorize",
		TokenURL:     "https://auth.example.com/token",
		UserInfoURL:  "https://auth.example.com/userinfo",
		RedirectURL:  "http://localhost:8080/auth/callback",
	}

	if err := ValidateOAuth2Config(valid); err != nil {
		t.Errorf("valid config should not error: %v", err)
	}

	fields := []struct {
		name  string
		clear func(config.OAuth2Config) config.OAuth2Config
	}{
		{"ClientID", func(c config.OAuth2Config) config.OAuth2Config { c.ClientID = ""; return c }},
		{"ClientSecret", func(c config.OAuth2Config) config.OAuth2Config { c.ClientSecret = ""; return c }},
		{"AuthURL", func(c config.OAuth2Config) config.OAuth2Config { c.AuthURL = ""; return c }},
		{"TokenURL", func(c config.OAuth2Config) config.OAuth2Config { c.TokenURL = ""; return c }},
		{"UserInfoURL", func(c config.OAuth2Config) config.OAuth2Config { c.UserInfoURL = ""; return c }},
		{"RedirectURL", func(c config.OAuth2Config) config.OAuth2Config { c.RedirectURL = ""; return c }},
	}

	for _, f := range fields {
		t.Run("missing_"+f.name, func(t *testing.T) {
			cfg := f.clear(valid)
			if err := ValidateOAuth2Config(cfg); err == nil {
				t.Errorf("expected error for missing %s", f.name)
			}
		})
	}
}

func TestOAuth2HandleCallbackTokenExchangeFailure(t *testing.T) {
	// Token server returns error
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "Authorization code is invalid or expired",
		})
	}))
	defer tokenServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}

	ctx := context.Background()
	_, err := auth.HandleCallback(ctx, "invalid-code")
	if err == nil {
		t.Error("expected error for invalid authorization code")
	}
	if !strings.Contains(err.Error(), "exchanging code for token") {
		t.Errorf("expected token exchange error, got: %v", err)
	}
}

func TestOAuth2HandleCallbackUserInfoFailure(t *testing.T) {
	// Token server succeeds
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	// UserInfo server returns error
	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	ctx := context.Background()
	_, err := auth.HandleCallback(ctx, "valid-code")
	if err == nil {
		t.Error("expected error for userinfo failure")
	}
	if !strings.Contains(err.Error(), "fetching user info") {
		t.Errorf("expected user info error, got: %v", err)
	}
}

func TestOAuth2HandleCallbackMissingUserIdentity(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	// UserInfo returns neither username nor email
	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sub":  "12345",
			"name": "Some User",
			// no preferred_username or email
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	ctx := context.Background()
	_, err := auth.HandleCallback(ctx, "valid-code")
	if err == nil {
		t.Error("expected error for missing user identity")
	}
	if !strings.Contains(err.Error(), "no username or email") {
		t.Errorf("expected 'no username or email' error, got: %v", err)
	}
}

func TestOAuth2HandleCallbackUserInfoUnauthorized(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	// UserInfo returns 401 (token expired/invalid)
	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid_token",
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	ctx := context.Background()
	_, err := auth.HandleCallback(ctx, "valid-code")
	if err == nil {
		t.Error("expected error for unauthorized userinfo request")
	}
}

func TestOAuth2HandleCallbackWithEditorGroup(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"preferred_username": "editor-user",
			"email":              "editor@example.com",
			"groups":             []string{"asiakirjat-editors", "other-group"},
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{
		GroupsClaim: "groups",
		AdminGroup:  "asiakirjat-admins",
		EditorGroup: "asiakirjat-editors",
		ViewerGroup: "asiakirjat-viewers",
	}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	ctx := context.Background()
	user, err := auth.HandleCallback(ctx, "mock-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.Role != "editor" {
		t.Errorf("expected role 'editor', got %q", user.Role)
	}
}

func TestOAuth2HandleCallbackNestedGroupsClaim(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	// Keycloak-style nested groups claim
	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"preferred_username": "keycloak-user",
			"email":              "keycloak@example.com",
			"realm_access": map[string]any{
				"roles": []string{"admin", "user"},
			},
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{
		GroupsClaim: "realm_access.roles",
		AdminGroup:  "admin",
		EditorGroup: "editor",
	}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	ctx := context.Background()
	user, err := auth.HandleCallback(ctx, "mock-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.Role != "admin" {
		t.Errorf("expected role 'admin' from nested claim, got %q", user.Role)
	}
}

func TestOAuth2ProjectAccessSync(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"preferred_username": "sync-user",
			"email":              "sync@example.com",
			"groups":             []string{"project-a-editors", "project-b-viewers"},
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	projectStore := sqlstore.NewProjectStore(db)
	accessStore := sqlstore.NewProjectAccessStore(db)
	groupMappingStore := sqlstore.NewAuthGroupMappingStore(db)
	logger := testutil.TestLogger()

	ctx := context.Background()

	// Create projects
	projectA := &database.Project{Slug: "project-a", Name: "Project A", IsPublic: true}
	projectB := &database.Project{Slug: "project-b", Name: "Project B", IsPublic: true}
	projectStore.Create(ctx, projectA)
	projectStore.Create(ctx, projectB)

	// Create group mappings
	groupMappingStore.Create(ctx, &database.AuthGroupMapping{
		AuthSource:      "oauth2",
		GroupIdentifier: "project-a-editors",
		ProjectID:       projectA.ID,
		Role:            "editor",
	})
	groupMappingStore.Create(ctx, &database.AuthGroupMapping{
		AuthSource:      "oauth2",
		GroupIdentifier: "project-b-viewers",
		ProjectID:       projectB.ID,
		Role:            "viewer",
	})

	auth := NewOAuth2Authenticator(config.OAuth2Config{
		GroupsClaim: "groups",
	}, userStore, logger)
	auth.SetStores(accessStore, groupMappingStore)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	user, err := auth.HandleCallback(ctx, "mock-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check project access was granted
	accessA, err := accessStore.GetAccess(ctx, projectA.ID, user.ID)
	if err != nil || accessA == nil {
		t.Fatal("expected access to project A")
	}
	if accessA.Role != "editor" {
		t.Errorf("expected editor role for project A, got %q", accessA.Role)
	}
	if accessA.Source != "oauth2" {
		t.Errorf("expected source 'oauth2', got %q", accessA.Source)
	}

	accessB, err := accessStore.GetAccess(ctx, projectB.ID, user.ID)
	if err != nil || accessB == nil {
		t.Fatal("expected access to project B")
	}
	if accessB.Role != "viewer" {
		t.Errorf("expected viewer role for project B, got %q", accessB.Role)
	}
}

func TestOAuth2ProjectAccessSyncRevocation(t *testing.T) {
	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	projectStore := sqlstore.NewProjectStore(db)
	accessStore := sqlstore.NewProjectAccessStore(db)
	groupMappingStore := sqlstore.NewAuthGroupMappingStore(db)
	logger := testutil.TestLogger()

	ctx := context.Background()

	// Create user and project
	user := &database.User{Username: "revoke-user", Email: "revoke@example.com", AuthSource: "oauth2", Role: "viewer"}
	userStore.Create(ctx, user)

	project := &database.Project{Slug: "revoke-project", Name: "Revoke Project", IsPublic: true}
	projectStore.Create(ctx, project)

	// Grant existing OAuth2-sourced access
	existingAccess := &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    user.ID,
		Role:      "editor",
		Source:    "oauth2",
	}
	accessStore.Grant(ctx, existingAccess)

	// Create group mapping (but user will NOT be in this group)
	groupMappingStore.Create(ctx, &database.AuthGroupMapping{
		AuthSource:      "oauth2",
		GroupIdentifier: "project-editors",
		ProjectID:       project.ID,
		Role:            "editor",
	})

	// Mock servers - user is NOT in project-editors group
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"preferred_username": "revoke-user",
			"email":              "revoke@example.com",
			"groups":             []string{"other-group"}, // Not in project-editors
		})
	}))
	defer userInfoServer.Close()

	auth := NewOAuth2Authenticator(config.OAuth2Config{
		GroupsClaim: "groups",
	}, userStore, logger)
	auth.SetStores(accessStore, groupMappingStore)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	_, err := auth.HandleCallback(ctx, "mock-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check access was revoked
	access, _ := accessStore.GetAccess(ctx, project.ID, user.ID)
	if access != nil {
		t.Error("expected OAuth2-sourced access to be revoked")
	}
}

func TestOAuth2ProjectAccessSyncHighestRoleWins(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	// User is in multiple groups that map to the same project
	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"preferred_username": "multi-group-user",
			"email":              "multi@example.com",
			"groups":             []string{"project-viewers", "project-editors"},
		})
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	projectStore := sqlstore.NewProjectStore(db)
	accessStore := sqlstore.NewProjectAccessStore(db)
	groupMappingStore := sqlstore.NewAuthGroupMappingStore(db)
	logger := testutil.TestLogger()

	ctx := context.Background()

	project := &database.Project{Slug: "multi-project", Name: "Multi Project", IsPublic: true}
	projectStore.Create(ctx, project)

	// Create two group mappings to same project with different roles
	groupMappingStore.Create(ctx, &database.AuthGroupMapping{
		AuthSource:      "oauth2",
		GroupIdentifier: "project-viewers",
		ProjectID:       project.ID,
		Role:            "viewer",
	})
	groupMappingStore.Create(ctx, &database.AuthGroupMapping{
		AuthSource:      "oauth2",
		GroupIdentifier: "project-editors",
		ProjectID:       project.ID,
		Role:            "editor",
	})

	auth := NewOAuth2Authenticator(config.OAuth2Config{
		GroupsClaim: "groups",
	}, userStore, logger)
	auth.SetStores(accessStore, groupMappingStore)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	user, err := auth.HandleCallback(ctx, "mock-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get the highest role (editor > viewer)
	access, err := accessStore.GetAccess(ctx, project.ID, user.ID)
	if err != nil || access == nil {
		t.Fatal("expected access to project")
	}
	if access.Role != "editor" {
		t.Errorf("expected highest role 'editor', got %q", access.Role)
	}
}

func TestOAuth2HandleCallbackMalformedJSON(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "mock-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	// UserInfo returns malformed JSON
	userInfoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json {"))
	}))
	defer userInfoServer.Close()

	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	logger := testutil.TestLogger()

	auth := NewOAuth2Authenticator(config.OAuth2Config{}, userStore, logger)
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}
	auth.userInfoURL = userInfoServer.URL

	ctx := context.Background()
	_, err := auth.HandleCallback(ctx, "valid-code")
	if err == nil {
		t.Error("expected error for malformed JSON response")
	}
	if !strings.Contains(err.Error(), "decoding user info") {
		t.Errorf("expected decoding error, got: %v", err)
	}
}

func TestOAuth2StateNotConsumedOnError(t *testing.T) {
	// This tests that state tokens aren't consumed if callback fails
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid_grant",
		})
	}))
	defer tokenServer.Close()

	auth := NewOAuth2Authenticator(config.OAuth2Config{}, nil, testutil.TestLogger())
	auth.oauthConfig = &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenServer.URL,
		},
	}

	// Generate state
	url, _ := auth.GenerateAuthURL()
	parts := strings.Split(url, "state=")
	state := strings.Split(parts[1], "&")[0]

	// Verify state is valid before callback
	// (but don't consume it - just check it exists in the map)
	auth.mu.Lock()
	exists := auth.states[state]
	auth.mu.Unlock()
	if !exists {
		t.Fatal("state should exist before callback")
	}

	// Callback fails (token exchange error) - state is NOT consumed by HandleCallback
	// Note: ValidateState is called separately in the handler, not in HandleCallback
	ctx := context.Background()
	_, err := auth.HandleCallback(ctx, "bad-code")
	if err == nil {
		t.Fatal("expected error")
	}

	// State should still be valid (not consumed by HandleCallback)
	if !auth.ValidateState(state) {
		t.Error("state should still be valid after failed callback")
	}
}

package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/go-ldap/ldap/v3"
	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
)

// mockLDAPConn is a mock LDAP connection for testing.
type mockLDAPConn struct {
	// bindFunc is called when Bind is invoked. Returns error if set.
	bindFunc func(username, password string) error
	// searchFunc is called when Search is invoked.
	searchFunc func(req *ldap.SearchRequest) (*ldap.SearchResult, error)
	// closed tracks if Close was called
	closed bool
}

func (m *mockLDAPConn) Bind(username, password string) error {
	if m.bindFunc != nil {
		return m.bindFunc(username, password)
	}
	return nil
}

func (m *mockLDAPConn) Search(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
	if m.searchFunc != nil {
		return m.searchFunc(req)
	}
	return &ldap.SearchResult{}, nil
}

func (m *mockLDAPConn) Close() error {
	m.closed = true
	return nil
}

// mockLDAPDialer is a mock dialer for testing.
type mockLDAPDialer struct {
	conn    *mockLDAPConn
	dialErr error
}

func (d *mockLDAPDialer) DialURL(addr string) (LDAPConn, error) {
	if d.dialErr != nil {
		return nil, d.dialErr
	}
	return d.conn, nil
}

// Helper to create a test LDAP entry
func createTestEntry(dn, uid, mail string, memberOf []string) *ldap.Entry {
	entry := ldap.NewEntry(dn, map[string][]string{
		"uid":      {uid},
		"mail":     {mail},
		"memberOf": memberOf,
	})
	return entry
}

func TestRenderUserFilter(t *testing.T) {
	tests := []struct {
		name     string
		template string
		username string
		expected string
	}{
		{
			name:     "simple uid filter",
			template: "(uid={{.Username}})",
			username: "alice",
			expected: "(uid=alice)",
		},
		{
			name:     "complex filter",
			template: "(&(objectClass=person)(uid={{.Username}}))",
			username: "bob",
			expected: "(&(objectClass=person)(uid=bob))",
		},
		{
			name:     "special characters escaped",
			template: "(uid={{.Username}})",
			username: "user*with(parens)",
			expected: `(uid=user\2awith\28parens\29)`,
		},
		{
			name:     "sAMAccountName filter",
			template: "(sAMAccountName={{.Username}})",
			username: "jdoe",
			expected: "(sAMAccountName=jdoe)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RenderUserFilter(tt.template, tt.username)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestRenderUserFilterInvalid(t *testing.T) {
	_, err := RenderUserFilter("{{.Invalid", "user")
	if err == nil {
		t.Error("expected error for invalid template")
	}
}

func TestMapGroupToRole(t *testing.T) {
	adminGroup := "cn=admins,ou=groups,dc=example,dc=com"
	editorGroup := "cn=editors,ou=groups,dc=example,dc=com"
	viewerGroup := "cn=viewers,ou=groups,dc=example,dc=com"

	tests := []struct {
		name        string
		memberOf    []string
		viewerGroup string
		expected    string
		allowed     bool
	}{
		{
			name:        "admin group member",
			memberOf:    []string{adminGroup, "cn=users,ou=groups,dc=example,dc=com"},
			viewerGroup: "",
			expected:    "admin",
			allowed:     true,
		},
		{
			name:        "editor group member",
			memberOf:    []string{editorGroup, "cn=users,ou=groups,dc=example,dc=com"},
			viewerGroup: "",
			expected:    "editor",
			allowed:     true,
		},
		{
			name:        "both admin and editor prefers admin",
			memberOf:    []string{editorGroup, adminGroup},
			viewerGroup: "",
			expected:    "admin",
			allowed:     true,
		},
		{
			name:        "no matching group defaults to viewer (backward compatible)",
			memberOf:    []string{"cn=users,ou=groups,dc=example,dc=com"},
			viewerGroup: "",
			expected:    "viewer",
			allowed:     true,
		},
		{
			name:        "empty groups defaults to viewer (backward compatible)",
			memberOf:    []string{},
			viewerGroup: "",
			expected:    "viewer",
			allowed:     true,
		},
		{
			name:        "nil groups defaults to viewer (backward compatible)",
			memberOf:    nil,
			viewerGroup: "",
			expected:    "viewer",
			allowed:     true,
		},
		{
			name:        "case insensitive match",
			memberOf:    []string{"CN=Admins,OU=Groups,DC=Example,DC=Com"},
			viewerGroup: "",
			expected:    "admin",
			allowed:     true,
		},
		{
			name:        "viewer group member when viewerGroup is set",
			memberOf:    []string{viewerGroup},
			viewerGroup: viewerGroup,
			expected:    "viewer",
			allowed:     true,
		},
		{
			name:        "not in any group when viewerGroup is set - denied",
			memberOf:    []string{"cn=users,ou=groups,dc=example,dc=com"},
			viewerGroup: viewerGroup,
			expected:    "",
			allowed:     false,
		},
		{
			name:        "empty groups when viewerGroup is set - denied",
			memberOf:    []string{},
			viewerGroup: viewerGroup,
			expected:    "",
			allowed:     false,
		},
		{
			name:        "admin group takes priority over viewer group",
			memberOf:    []string{viewerGroup, adminGroup},
			viewerGroup: viewerGroup,
			expected:    "admin",
			allowed:     true,
		},
		{
			name:        "editor group takes priority over viewer group",
			memberOf:    []string{viewerGroup, editorGroup},
			viewerGroup: viewerGroup,
			expected:    "editor",
			allowed:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, allowed := MapGroupToRole(tt.memberOf, adminGroup, editorGroup, tt.viewerGroup)
			if got != tt.expected {
				t.Errorf("expected role %q, got %q", tt.expected, got)
			}
			if allowed != tt.allowed {
				t.Errorf("expected allowed=%v, got allowed=%v", tt.allowed, allowed)
			}
		})
	}
}

func TestValidateLDAPConfig(t *testing.T) {
	valid := config.LDAPConfig{
		Enabled:      true,
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "secret",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
	}

	if err := ValidateLDAPConfig(valid); err != nil {
		t.Errorf("valid config should not error: %v", err)
	}

	// Missing URL
	noURL := valid
	noURL.URL = ""
	if err := ValidateLDAPConfig(noURL); err == nil {
		t.Error("expected error for missing URL")
	}

	// Missing BindDN
	noBindDN := valid
	noBindDN.BindDN = ""
	if err := ValidateLDAPConfig(noBindDN); err == nil {
		t.Error("expected error for missing BindDN")
	}

	// Missing BaseDN
	noBaseDN := valid
	noBaseDN.BaseDN = ""
	if err := ValidateLDAPConfig(noBaseDN); err == nil {
		t.Error("expected error for missing BaseDN")
	}

	// Missing UserFilter
	noFilter := valid
	noFilter.UserFilter = ""
	if err := ValidateLDAPConfig(noFilter); err == nil {
		t.Error("expected error for missing UserFilter")
	}
}

func TestLDAPAuthenticatorName(t *testing.T) {
	auth := NewLDAPAuthenticator(config.LDAPConfig{}, nil, nil)
	if auth.Name() != "ldap" {
		t.Errorf("expected name 'ldap', got %q", auth.Name())
	}
}

// Integration tests using mock LDAP

func setupLDAPTest(t *testing.T) (*sqlstore.UserStore, *sqlstore.ProjectAccessStore, *sqlstore.AuthGroupMappingStore, *sqlstore.ProjectStore) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	accessStore := sqlstore.NewProjectAccessStore(db)
	mappingStore := sqlstore.NewAuthGroupMappingStore(db)
	projectStore := sqlstore.NewProjectStore(db)
	return userStore, accessStore, mappingStore, projectStore
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestLDAPAuthenticateSuccess(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
		AdminGroup:   "cn=admins,ou=groups,dc=example,dc=com",
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			// Service account bind
			if username == cfg.BindDN && password == cfg.BindPassword {
				return nil
			}
			// User bind
			if username == "uid=alice,ou=users,dc=example,dc=com" && password == "alicepass" {
				return nil
			}
			return errors.New("invalid credentials")
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry(
						"uid=alice,ou=users,dc=example,dc=com",
						"alice",
						"alice@example.com",
						[]string{"cn=admins,ou=groups,dc=example,dc=com"},
					),
				},
			}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	user, err := auth.Authenticate(ctx, "alice", "alicepass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.Username != "alice" {
		t.Errorf("expected username 'alice', got %q", user.Username)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", user.Email)
	}
	if user.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", user.Role)
	}
	if user.AuthSource != "ldap" {
		t.Errorf("expected auth_source 'ldap', got %q", user.AuthSource)
	}
	if !mockConn.closed {
		t.Error("expected connection to be closed")
	}
}

func TestLDAPAuthenticateEmptyPassword(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:        "ldap://localhost:389",
		BindDN:     "cn=admin,dc=example,dc=com",
		BaseDN:     "dc=example,dc=com",
		UserFilter: "(uid={{.Username}})",
	}

	auth := NewLDAPAuthenticator(cfg, userStore, testLogger())

	ctx := context.Background()
	_, err := auth.Authenticate(ctx, "alice", "")
	if err == nil {
		t.Error("expected error for empty password")
	}
	if err.Error() != "empty password" {
		t.Errorf("expected 'empty password' error, got %q", err.Error())
	}
}

func TestLDAPAuthenticateConnectionFailed(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:        "ldap://localhost:389",
		BindDN:     "cn=admin,dc=example,dc=com",
		BaseDN:     "dc=example,dc=com",
		UserFilter: "(uid={{.Username}})",
	}

	dialer := &mockLDAPDialer{dialErr: errors.New("connection refused")}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	_, err := auth.Authenticate(ctx, "alice", "password")
	if err == nil {
		t.Error("expected error for connection failure")
	}
	if !errors.Is(err, errors.Unwrap(err)) && err.Error() != "connecting to LDAP: connection refused" {
		// Just check it contains the right message
		if !contains(err.Error(), "connecting to LDAP") {
			t.Errorf("expected connection error, got %q", err.Error())
		}
	}
}

func TestLDAPAuthenticateServiceBindFailed(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "wrongpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			return errors.New("invalid credentials")
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	_, err := auth.Authenticate(ctx, "alice", "password")
	if err == nil {
		t.Error("expected error for service bind failure")
	}
	if !contains(err.Error(), "service account bind failed") {
		t.Errorf("expected service bind error, got %q", err.Error())
	}
}

func TestLDAPAuthenticateUserNotFound(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			if username == cfg.BindDN {
				return nil
			}
			return errors.New("invalid credentials")
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			// Return empty result - user not found
			return &ldap.SearchResult{Entries: []*ldap.Entry{}}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	_, err := auth.Authenticate(ctx, "nonexistent", "password")
	if err == nil {
		t.Error("expected error for user not found")
	}
	if !contains(err.Error(), "user not found") {
		t.Errorf("expected 'user not found' error, got %q", err.Error())
	}
}

func TestLDAPAuthenticateInvalidUserPassword(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			// Service bind succeeds
			if username == cfg.BindDN && password == cfg.BindPassword {
				return nil
			}
			// User bind fails - wrong password
			return errors.New("invalid credentials")
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry("uid=alice,ou=users,dc=example,dc=com", "alice", "alice@example.com", nil),
				},
			}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	_, err := auth.Authenticate(ctx, "alice", "wrongpassword")
	if err == nil {
		t.Error("expected error for invalid password")
	}
	if !contains(err.Error(), "invalid LDAP credentials") {
		t.Errorf("expected 'invalid LDAP credentials' error, got %q", err.Error())
	}
}

func TestLDAPAuthenticateUserNotInAllowedGroup(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
		ViewerGroup:  "cn=viewers,ou=groups,dc=example,dc=com", // Require group membership
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			return nil // All binds succeed
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry(
						"uid=alice,ou=users,dc=example,dc=com",
						"alice",
						"alice@example.com",
						[]string{"cn=users,ou=groups,dc=example,dc=com"}, // Not in any allowed group
					),
				},
			}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	_, err := auth.Authenticate(ctx, "alice", "password")
	if err == nil {
		t.Error("expected error for user not in allowed group")
	}
	if !contains(err.Error(), "not in any allowed group") {
		t.Errorf("expected 'not in any allowed group' error, got %q", err.Error())
	}
}

func TestLDAPAuthenticateEditorRole(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
		EditorGroup:  "cn=editors,ou=groups,dc=example,dc=com",
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			return nil
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry(
						"uid=bob,ou=users,dc=example,dc=com",
						"bob",
						"bob@example.com",
						[]string{"cn=editors,ou=groups,dc=example,dc=com"},
					),
				},
			}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	user, err := auth.Authenticate(ctx, "bob", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.Role != "editor" {
		t.Errorf("expected role 'editor', got %q", user.Role)
	}
}

func TestLDAPAuthenticateViewerRole(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
		// No admin/editor groups configured, user gets viewer
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			return nil
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry(
						"uid=viewer,ou=users,dc=example,dc=com",
						"viewer",
						"viewer@example.com",
						nil,
					),
				},
			}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	user, err := auth.Authenticate(ctx, "viewer", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.Role != "viewer" {
		t.Errorf("expected role 'viewer', got %q", user.Role)
	}
}

func TestLDAPUserProvisioningCreatesNewUser(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			return nil
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry(
						"uid=newuser,ou=users,dc=example,dc=com",
						"newuser",
						"newuser@example.com",
						nil,
					),
				},
			}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()

	// Verify user doesn't exist
	_, err := userStore.GetByUsername(ctx, "newuser")
	if err == nil {
		t.Fatal("expected user to not exist before auth")
	}

	// Authenticate - should create user
	user, err := auth.Authenticate(ctx, "newuser", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify user was created in database
	dbUser, err := userStore.GetByUsername(ctx, "newuser")
	if err != nil {
		t.Fatalf("expected user to exist after auth: %v", err)
	}
	if dbUser.ID != user.ID {
		t.Error("expected returned user to match database user")
	}
	if dbUser.AuthSource != "ldap" {
		t.Errorf("expected auth_source 'ldap', got %q", dbUser.AuthSource)
	}
}

func TestLDAPUserProvisioningUpdatesExistingUser(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
		AdminGroup:   "cn=admins,ou=groups,dc=example,dc=com",
	}

	ctx := context.Background()

	// Create existing user with viewer role
	existingUser := &database.User{
		Username:   "promoted",
		Email:      "old@example.com",
		AuthSource: "ldap",
		Role:       "viewer",
	}
	userStore.Create(ctx, existingUser)

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			return nil
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry(
						"uid=promoted,ou=users,dc=example,dc=com",
						"promoted",
						"new@example.com", // Updated email
						[]string{"cn=admins,ou=groups,dc=example,dc=com"}, // Now admin group
					),
				},
			}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	// Authenticate - should update email but preserve role
	user, err := auth.Authenticate(ctx, "promoted", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify role was preserved (not overwritten by LDAP group)
	if user.Role != "viewer" {
		t.Errorf("expected role to be preserved as 'viewer', got %q", user.Role)
	}
	// Verify email was updated
	if user.Email != "new@example.com" {
		t.Errorf("expected email to be updated to 'new@example.com', got %q", user.Email)
	}

	// Verify changes persisted in database
	dbUser, _ := userStore.GetByUsername(ctx, "promoted")
	if dbUser.Role != "viewer" {
		t.Errorf("expected persisted role 'viewer', got %q", dbUser.Role)
	}
	if dbUser.Email != "new@example.com" {
		t.Errorf("expected persisted email 'new@example.com', got %q", dbUser.Email)
	}
}

func TestLDAPProjectAccessSync(t *testing.T) {
	userStore, accessStore, mappingStore, projectStore := setupLDAPTest(t)

	ctx := context.Background()

	// Create test projects
	project1 := &database.Project{Slug: "proj1", Name: "Project 1", Visibility: database.VisibilityCustom}
	project2 := &database.Project{Slug: "proj2", Name: "Project 2", Visibility: database.VisibilityCustom}
	projectStore.Create(ctx, project1)
	projectStore.Create(ctx, project2)

	// Create group mappings
	devGroup := "cn=developers,ou=groups,dc=example,dc=com"
	mappingStore.Create(ctx, &database.AuthGroupMapping{
		AuthSource:      "ldap",
		GroupIdentifier: devGroup,
		ProjectID:       project1.ID,
		Role:            "editor",
	})
	mappingStore.Create(ctx, &database.AuthGroupMapping{
		AuthSource:      "ldap",
		GroupIdentifier: devGroup,
		ProjectID:       project2.ID,
		Role:            "viewer",
	})

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			return nil
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry(
						"uid=developer,ou=users,dc=example,dc=com",
						"developer",
						"dev@example.com",
						[]string{devGroup},
					),
				},
			}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)
	auth.SetStores(accessStore, mappingStore, nil)

	// Authenticate - should sync project access
	user, err := auth.Authenticate(ctx, "developer", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify project access was granted
	access1, err := accessStore.GetAccess(ctx, project1.ID, user.ID)
	if err != nil {
		t.Fatalf("expected access to project1: %v", err)
	}
	if access1.Role != "editor" {
		t.Errorf("expected editor role for project1, got %q", access1.Role)
	}
	if access1.Source != "ldap" {
		t.Errorf("expected source 'ldap', got %q", access1.Source)
	}

	access2, err := accessStore.GetAccess(ctx, project2.ID, user.ID)
	if err != nil {
		t.Fatalf("expected access to project2: %v", err)
	}
	if access2.Role != "viewer" {
		t.Errorf("expected viewer role for project2, got %q", access2.Role)
	}
}

func TestLDAPProjectAccessSyncRevokesRemovedGroups(t *testing.T) {
	userStore, accessStore, mappingStore, projectStore := setupLDAPTest(t)

	ctx := context.Background()

	// Create test project
	project := &database.Project{Slug: "revoke-test", Name: "Revoke Test", Visibility: database.VisibilityCustom}
	projectStore.Create(ctx, project)

	// Create group mapping
	devGroup := "cn=developers,ou=groups,dc=example,dc=com"
	mappingStore.Create(ctx, &database.AuthGroupMapping{
		AuthSource:      "ldap",
		GroupIdentifier: devGroup,
		ProjectID:       project.ID,
		Role:            "editor",
	})

	// Create user with existing LDAP access
	user := &database.User{
		Username:   "ex-dev",
		Email:      "exdev@example.com",
		AuthSource: "ldap",
		Role:       "viewer",
	}
	userStore.Create(ctx, user)
	accessStore.Grant(ctx, &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    user.ID,
		Role:      "editor",
		Source:    "ldap",
	})

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			return nil
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry(
						"uid=ex-dev,ou=users,dc=example,dc=com",
						"ex-dev",
						"exdev@example.com",
						[]string{}, // No longer in developers group
					),
				},
			}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)
	auth.SetStores(accessStore, mappingStore, nil)

	// Authenticate - should revoke access
	_, err := auth.Authenticate(ctx, "ex-dev", "password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify project access was revoked
	access, _ := accessStore.GetAccess(ctx, project.ID, user.ID)
	if access != nil {
		t.Error("expected access to be revoked when user removed from group")
	}
}

func TestLDAPSearchFailed(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
	}

	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			return nil
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return nil, errors.New("search timeout")
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	_, err := auth.Authenticate(ctx, "alice", "password")
	if err == nil {
		t.Error("expected error for search failure")
	}
	if !contains(err.Error(), "LDAP search failed") {
		t.Errorf("expected 'LDAP search failed' error, got %q", err.Error())
	}
}

func TestResolveTransitiveGroups(t *testing.T) {
	// A -> B -> C (linear chain)
	groupA := "cn=team-a,ou=groups,dc=example,dc=com"
	groupB := "cn=team-b,ou=groups,dc=example,dc=com"
	groupC := "cn=team-c,ou=groups,dc=example,dc=com"

	mockConn := &mockLDAPConn{
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			switch strings.ToLower(req.BaseDN) {
			case strings.ToLower(groupA):
				return &ldap.SearchResult{
					Entries: []*ldap.Entry{
						ldap.NewEntry(groupA, map[string][]string{"memberOf": {groupB}}),
					},
				}, nil
			case strings.ToLower(groupB):
				return &ldap.SearchResult{
					Entries: []*ldap.Entry{
						ldap.NewEntry(groupB, map[string][]string{"memberOf": {groupC}}),
					},
				}, nil
			case strings.ToLower(groupC):
				return &ldap.SearchResult{
					Entries: []*ldap.Entry{
						ldap.NewEntry(groupC, map[string][]string{}),
					},
				}, nil
			}
			return &ldap.SearchResult{}, nil
		},
	}

	result := resolveTransitiveGroups(mockConn, []string{groupA}, "", testLogger())

	if len(result) != 3 {
		t.Fatalf("expected 3 groups, got %d: %v", len(result), result)
	}

	resultSet := make(map[string]bool)
	for _, g := range result {
		resultSet[strings.ToLower(g)] = true
	}
	for _, expected := range []string{groupA, groupB, groupC} {
		if !resultSet[strings.ToLower(expected)] {
			t.Errorf("expected %q in result set", expected)
		}
	}
}

func TestResolveTransitiveGroupsCycle(t *testing.T) {
	// A -> B -> A (circular)
	groupA := "cn=team-a,ou=groups,dc=example,dc=com"
	groupB := "cn=team-b,ou=groups,dc=example,dc=com"

	mockConn := &mockLDAPConn{
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			switch strings.ToLower(req.BaseDN) {
			case strings.ToLower(groupA):
				return &ldap.SearchResult{
					Entries: []*ldap.Entry{
						ldap.NewEntry(groupA, map[string][]string{"memberOf": {groupB}}),
					},
				}, nil
			case strings.ToLower(groupB):
				return &ldap.SearchResult{
					Entries: []*ldap.Entry{
						ldap.NewEntry(groupB, map[string][]string{"memberOf": {groupA}}),
					},
				}, nil
			}
			return &ldap.SearchResult{}, nil
		},
	}

	result := resolveTransitiveGroups(mockConn, []string{groupA}, "", testLogger())

	if len(result) != 2 {
		t.Fatalf("expected 2 groups (cycle handled), got %d: %v", len(result), result)
	}
}

func TestResolveTransitiveGroupsLimit(t *testing.T) {
	// Deep chain: group-0 -> group-1 -> ... -> group-99
	// Should stop at 50 iterations
	searchCount := 0
	mockConn := &mockLDAPConn{
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			searchCount++
			// Each group points to the next
			parent := fmt.Sprintf("cn=group-%d,ou=groups,dc=example,dc=com", searchCount)
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					ldap.NewEntry(req.BaseDN, map[string][]string{"memberOf": {parent}}),
				},
			}, nil
		},
	}

	start := "cn=group-0,ou=groups,dc=example,dc=com"
	result := resolveTransitiveGroups(mockConn, []string{start}, "", testLogger())

	if searchCount > 50 {
		t.Errorf("expected at most 50 LDAP lookups, got %d", searchCount)
	}
	// Should have start + 50 parents = 51 groups
	if len(result) != 51 {
		t.Errorf("expected 51 groups, got %d", len(result))
	}
}

func TestResolveTransitiveGroupsPrefix(t *testing.T) {
	// team-a -> external-admins -> top-editors
	// With prefix "team-", only team-a (CN starts with "team-") is recursed into
	groupA := "cn=team-a,ou=groups,dc=example,dc=com"
	groupB := "cn=external-admins,ou=groups,dc=example,dc=com"
	groupC := "cn=top-editors,ou=groups,dc=example,dc=com"

	searchedDNs := make(map[string]bool)
	mockConn := &mockLDAPConn{
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			searchedDNs[strings.ToLower(req.BaseDN)] = true
			switch strings.ToLower(req.BaseDN) {
			case strings.ToLower(groupA):
				return &ldap.SearchResult{
					Entries: []*ldap.Entry{
						ldap.NewEntry(groupA, map[string][]string{"memberOf": {groupB}}),
					},
				}, nil
			case strings.ToLower(groupB):
				return &ldap.SearchResult{
					Entries: []*ldap.Entry{
						ldap.NewEntry(groupB, map[string][]string{"memberOf": {groupC}}),
					},
				}, nil
			}
			return &ldap.SearchResult{}, nil
		},
	}

	result := resolveTransitiveGroups(mockConn, []string{groupA}, "team-", testLogger())

	// groupA CN is "team-a" → matches prefix "team-", recursed, discovers groupB
	// groupB CN is "external-admins" → does NOT match, included but not recursed
	// groupC is never discovered because groupB was not recursed
	if len(result) != 2 {
		t.Fatalf("expected 2 groups (A + B), got %d: %v", len(result), result)
	}

	// Verify groupB was NOT searched (CN doesn't match prefix)
	if searchedDNs[strings.ToLower(groupB)] {
		t.Error("groupB should not have been searched (CN doesn't match prefix)")
	}
}

func TestGroupCN(t *testing.T) {
	tests := []struct {
		dn       string
		expected string
	}{
		{"cn=team-a,ou=groups,dc=example,dc=com", "team-a"},
		{"CN=Editors,OU=Groups,DC=Example,DC=Com", "Editors"},
		{"ou=groups,dc=example,dc=com", ""},
		{"invalid", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := groupCN(tt.dn)
		if got != tt.expected {
			t.Errorf("groupCN(%q) = %q, want %q", tt.dn, got, tt.expected)
		}
	}
}

func TestLDAPAuthRecursiveGroups(t *testing.T) {
	userStore, _, _, _ := setupLDAPTest(t)

	teamA := "cn=team-a,ou=groups,dc=example,dc=com"
	editors := "cn=editors,ou=groups,dc=example,dc=com"

	cfg := config.LDAPConfig{
		URL:             "ldap://localhost:389",
		BindDN:          "cn=admin,dc=example,dc=com",
		BindPassword:    "adminpass",
		BaseDN:          "dc=example,dc=com",
		UserFilter:      "(uid={{.Username}})",
		EditorGroup:     editors,
		RecursiveGroups: true,
	}

	bindCount := 0
	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error {
			bindCount++
			if username == cfg.BindDN && password == cfg.BindPassword {
				return nil
			}
			if username == "uid=alice,ou=users,dc=example,dc=com" && password == "alicepass" {
				return nil
			}
			return errors.New("invalid credentials")
		},
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			// User search (subtree scope)
			if req.Scope == ldap.ScopeWholeSubtree {
				return &ldap.SearchResult{
					Entries: []*ldap.Entry{
						createTestEntry(
							"uid=alice,ou=users,dc=example,dc=com",
							"alice",
							"alice@example.com",
							[]string{teamA}, // Only direct member of team-a
						),
					},
				}, nil
			}
			// Group search (base scope for recursive resolution)
			if req.Scope == ldap.ScopeBaseObject {
				switch strings.ToLower(req.BaseDN) {
				case strings.ToLower(teamA):
					return &ldap.SearchResult{
						Entries: []*ldap.Entry{
							ldap.NewEntry(teamA, map[string][]string{"memberOf": {editors}}),
						},
					}, nil
				case strings.ToLower(editors):
					return &ldap.SearchResult{
						Entries: []*ldap.Entry{
							ldap.NewEntry(editors, map[string][]string{}),
						},
					}, nil
				}
			}
			return &ldap.SearchResult{}, nil
		},
	}

	dialer := &mockLDAPDialer{conn: mockConn}
	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), dialer)

	ctx := context.Background()
	user, err := auth.Authenticate(ctx, "alice", "alicepass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// User should get editor role via recursive group: team-a -> editors
	if user.Role != "editor" {
		t.Errorf("expected role 'editor' via recursive group, got %q", user.Role)
	}
}

func TestRoleHigher(t *testing.T) {
	tests := []struct {
		a, b     string
		expected bool
	}{
		{"admin", "editor", true},
		{"admin", "viewer", true},
		{"admin", "", true},
		{"editor", "viewer", true},
		{"editor", "", true},
		{"viewer", "", true},
		{"editor", "admin", false},
		{"viewer", "admin", false},
		{"viewer", "editor", false},
		{"", "admin", false},
		{"admin", "admin", false},
		{"editor", "editor", false},
	}

	for _, tt := range tests {
		got := roleHigher(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("roleHigher(%q, %q) = %v, expected %v", tt.a, tt.b, got, tt.expected)
		}
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

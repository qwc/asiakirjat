package auth

import (
	"testing"

	"github.com/qwc/asiakirjat/internal/config"
)

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

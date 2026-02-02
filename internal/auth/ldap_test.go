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

	tests := []struct {
		name     string
		memberOf []string
		expected string
	}{
		{
			name:     "admin group member",
			memberOf: []string{adminGroup, "cn=users,ou=groups,dc=example,dc=com"},
			expected: "admin",
		},
		{
			name:     "editor group member",
			memberOf: []string{editorGroup, "cn=users,ou=groups,dc=example,dc=com"},
			expected: "editor",
		},
		{
			name:     "both admin and editor prefers admin",
			memberOf: []string{editorGroup, adminGroup},
			expected: "admin",
		},
		{
			name:     "no matching group defaults to viewer",
			memberOf: []string{"cn=users,ou=groups,dc=example,dc=com"},
			expected: "viewer",
		},
		{
			name:     "empty groups defaults to viewer",
			memberOf: []string{},
			expected: "viewer",
		},
		{
			name:     "nil groups defaults to viewer",
			memberOf: nil,
			expected: "viewer",
		},
		{
			name:     "case insensitive match",
			memberOf: []string{"CN=Admins,OU=Groups,DC=Example,DC=Com"},
			expected: "admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapGroupToRole(tt.memberOf, adminGroup, editorGroup)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
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

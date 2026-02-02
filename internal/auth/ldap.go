package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"text/template"

	"github.com/go-ldap/ldap/v3"
	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
)

// LDAPAuthenticator authenticates users against an LDAP directory.
type LDAPAuthenticator struct {
	config config.LDAPConfig
	users  store.UserStore
	logger *slog.Logger
}

// NewLDAPAuthenticator creates a new LDAP authenticator.
func NewLDAPAuthenticator(cfg config.LDAPConfig, users store.UserStore, logger *slog.Logger) *LDAPAuthenticator {
	return &LDAPAuthenticator{
		config: cfg,
		users:  users,
		logger: logger,
	}
}

func (a *LDAPAuthenticator) Name() string {
	return "ldap"
}

// Authenticate verifies credentials against LDAP and auto-provisions users.
func (a *LDAPAuthenticator) Authenticate(ctx context.Context, username, password string) (*database.User, error) {
	if password == "" {
		return nil, fmt.Errorf("empty password")
	}

	// Connect to LDAP server
	conn, err := ldap.DialURL(a.config.URL)
	if err != nil {
		return nil, fmt.Errorf("connecting to LDAP: %w", err)
	}
	defer conn.Close()

	// Bind with service account to search
	if err := conn.Bind(a.config.BindDN, a.config.BindPassword); err != nil {
		return nil, fmt.Errorf("service account bind failed: %w", err)
	}

	// Build user filter
	filter, err := RenderUserFilter(a.config.UserFilter, username)
	if err != nil {
		return nil, fmt.Errorf("rendering user filter: %w", err)
	}

	// Search for the user
	searchReq := ldap.NewSearchRequest(
		a.config.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		1, // size limit
		0, // time limit
		false,
		filter,
		[]string{"dn", "uid", "mail", "memberOf"},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %w", err)
	}

	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("user not found in LDAP")
	}

	entry := result.Entries[0]
	userDN := entry.DN

	// Bind as the user to verify password
	if err := conn.Bind(userDN, password); err != nil {
		return nil, fmt.Errorf("invalid LDAP credentials")
	}

	// Determine role from group membership
	memberOf := entry.GetAttributeValues("memberOf")
	role := MapGroupToRole(memberOf, a.config.AdminGroup, a.config.EditorGroup)

	email := entry.GetAttributeValue("mail")

	// Auto-provision or update user
	user, err := a.provisionUser(ctx, username, email, role)
	if err != nil {
		return nil, fmt.Errorf("provisioning user: %w", err)
	}

	return user, nil
}

// provisionUser creates or updates a user record for an LDAP-authenticated user.
func (a *LDAPAuthenticator) provisionUser(ctx context.Context, username, email, role string) (*database.User, error) {
	existing, err := a.users.GetByUsername(ctx, username)
	if err == nil && existing != nil {
		// Update role and email if changed
		if existing.Role != role || existing.Email != email {
			existing.Role = role
			existing.Email = email
			if err := a.users.Update(ctx, existing); err != nil {
				a.logger.Warn("updating LDAP user", "username", username, "error", err)
			}
		}
		return existing, nil
	}

	// Create new user
	user := &database.User{
		Username:   username,
		Email:      email,
		AuthSource: "ldap",
		Role:       role,
	}
	if err := a.users.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("creating LDAP user: %w", err)
	}

	a.logger.Info("auto-provisioned LDAP user", "username", username, "role", role)
	return user, nil
}

// RenderUserFilter applies the username to the LDAP user filter template.
// The template uses {{.Username}} as a placeholder.
func RenderUserFilter(filterTemplate, username string) (string, error) {
	tmpl, err := template.New("filter").Parse(filterTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing filter template: %w", err)
	}

	var buf strings.Builder
	data := struct{ Username string }{Username: ldap.EscapeFilter(username)}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing filter template: %w", err)
	}

	return buf.String(), nil
}

// MapGroupToRole determines a user's role based on LDAP group membership.
// Returns "admin" if the user is in the admin group, "editor" if in the editor group,
// and "viewer" as the default.
func MapGroupToRole(memberOf []string, adminGroup, editorGroup string) string {
	for _, group := range memberOf {
		if strings.EqualFold(group, adminGroup) {
			return "admin"
		}
	}
	for _, group := range memberOf {
		if strings.EqualFold(group, editorGroup) {
			return "editor"
		}
	}
	return "viewer"
}

// ValidateLDAPConfig checks that required LDAP config fields are set.
func ValidateLDAPConfig(cfg config.LDAPConfig) error {
	if cfg.URL == "" {
		return fmt.Errorf("LDAP URL is required")
	}
	if cfg.BindDN == "" {
		return fmt.Errorf("LDAP bind DN is required")
	}
	if cfg.BaseDN == "" {
		return fmt.Errorf("LDAP base DN is required")
	}
	if cfg.UserFilter == "" {
		return fmt.Errorf("LDAP user filter is required")
	}
	return nil
}

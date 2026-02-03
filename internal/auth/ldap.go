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

// LDAPConn represents an LDAP connection interface for mockable testing.
type LDAPConn interface {
	Bind(username, password string) error
	Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error)
	Close() error
}

// LDAPDialer creates LDAP connections. This interface allows mocking in tests.
type LDAPDialer interface {
	DialURL(addr string) (LDAPConn, error)
}

// realLDAPDialer is the production implementation using go-ldap.
type realLDAPDialer struct{}

func (d *realLDAPDialer) DialURL(addr string) (LDAPConn, error) {
	return ldap.DialURL(addr)
}

// DefaultLDAPDialer returns the default production LDAP dialer.
func DefaultLDAPDialer() LDAPDialer {
	return &realLDAPDialer{}
}

// LDAPAuthenticator authenticates users against an LDAP directory.
type LDAPAuthenticator struct {
	config        config.LDAPConfig
	users         store.UserStore
	access        store.ProjectAccessStore
	groupMappings store.AuthGroupMappingStore
	logger        *slog.Logger
	dialer        LDAPDialer
}

// NewLDAPAuthenticator creates a new LDAP authenticator.
func NewLDAPAuthenticator(cfg config.LDAPConfig, users store.UserStore, logger *slog.Logger) *LDAPAuthenticator {
	return &LDAPAuthenticator{
		config: cfg,
		users:  users,
		logger: logger,
		dialer: DefaultLDAPDialer(),
	}
}

// NewLDAPAuthenticatorWithDialer creates a new LDAP authenticator with a custom dialer (for testing).
func NewLDAPAuthenticatorWithDialer(cfg config.LDAPConfig, users store.UserStore, logger *slog.Logger, dialer LDAPDialer) *LDAPAuthenticator {
	return &LDAPAuthenticator{
		config: cfg,
		users:  users,
		logger: logger,
		dialer: dialer,
	}
}

// SetStores sets the access and group mapping stores for project-level access sync.
// This is called after authenticator creation to avoid circular dependencies.
func (a *LDAPAuthenticator) SetStores(access store.ProjectAccessStore, groupMappings store.AuthGroupMappingStore) {
	a.access = access
	a.groupMappings = groupMappings
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
	conn, err := a.dialer.DialURL(a.config.URL)
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
	role, allowed := MapGroupToRole(memberOf, a.config.AdminGroup, a.config.EditorGroup, a.config.ViewerGroup)
	if !allowed {
		return nil, fmt.Errorf("user not in any allowed group")
	}

	email := entry.GetAttributeValue("mail")

	// Auto-provision or update user
	user, err := a.provisionUser(ctx, username, email, role)
	if err != nil {
		return nil, fmt.Errorf("provisioning user: %w", err)
	}

	// Sync project access based on group mappings
	if a.access != nil && a.groupMappings != nil {
		if err := a.syncProjectAccess(ctx, user, memberOf); err != nil {
			a.logger.Warn("syncing LDAP project access", "username", username, "error", err)
		}
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

// syncProjectAccess synchronizes project access for a user based on their LDAP group membership.
// It grants access for groups the user is a member of and revokes access for groups they're no longer in.
func (a *LDAPAuthenticator) syncProjectAccess(ctx context.Context, user *database.User, memberOf []string) error {
	// Get all LDAP group mappings from the database
	mappings, err := a.groupMappings.ListBySource(ctx, "ldap")
	if err != nil {
		return fmt.Errorf("listing LDAP group mappings: %w", err)
	}

	if len(mappings) == 0 {
		return nil
	}

	// Build a set of user's groups for fast lookup (case-insensitive)
	userGroups := make(map[string]bool)
	for _, g := range memberOf {
		userGroups[strings.ToLower(g)] = true
	}

	// Track which projects the user should have access to via LDAP
	grantedProjects := make(map[int64]string) // project_id -> highest role

	for _, mapping := range mappings {
		if userGroups[strings.ToLower(mapping.GroupIdentifier)] {
			// User is in this group - grant access
			currentRole := grantedProjects[mapping.ProjectID]
			if roleHigher(mapping.Role, currentRole) {
				grantedProjects[mapping.ProjectID] = mapping.Role
			}
		}
	}

	// Get existing LDAP-sourced access for this user
	existingAccess, err := a.access.ListByUserAndSource(ctx, user.ID, "ldap")
	if err != nil {
		return fmt.Errorf("listing existing LDAP access: %w", err)
	}

	existingProjects := make(map[int64]string)
	for _, access := range existingAccess {
		existingProjects[access.ProjectID] = access.Role
	}

	// Grant new or update existing access
	for projectID, role := range grantedProjects {
		if existingRole, exists := existingProjects[projectID]; !exists || existingRole != role {
			access := &database.ProjectAccess{
				ProjectID: projectID,
				UserID:    user.ID,
				Role:      role,
				Source:    "ldap",
			}
			if err := a.access.Grant(ctx, access); err != nil {
				a.logger.Warn("granting LDAP project access", "project_id", projectID, "error", err)
			}
		}
	}

	// Revoke access for projects no longer granted by LDAP
	for projectID := range existingProjects {
		if _, shouldHave := grantedProjects[projectID]; !shouldHave {
			if err := a.access.RevokeBySource(ctx, projectID, user.ID, "ldap"); err != nil {
				a.logger.Warn("revoking LDAP project access", "project_id", projectID, "error", err)
			}
		}
	}

	return nil
}

// roleHigher returns true if role a is higher priority than role b
func roleHigher(a, b string) bool {
	priority := map[string]int{"admin": 3, "editor": 2, "viewer": 1, "": 0}
	return priority[a] > priority[b]
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
// Returns the role and whether the user is allowed.
// If viewerGroup is set, the user must be in at least one of the configured groups.
// If viewerGroup is empty, any user is allowed and defaults to "viewer" (backward compatible).
func MapGroupToRole(memberOf []string, adminGroup, editorGroup, viewerGroup string) (string, bool) {
	// Check for admin group first (highest priority)
	for _, group := range memberOf {
		if adminGroup != "" && strings.EqualFold(group, adminGroup) {
			return "admin", true
		}
	}
	// Check for editor group
	for _, group := range memberOf {
		if editorGroup != "" && strings.EqualFold(group, editorGroup) {
			return "editor", true
		}
	}
	// Check for viewer group
	for _, group := range memberOf {
		if viewerGroup != "" && strings.EqualFold(group, viewerGroup) {
			return "viewer", true
		}
	}

	// If viewerGroup is set, user must be in one of the groups to be allowed
	if viewerGroup != "" {
		return "", false
	}

	// Backward compatible: if no viewerGroup configured, allow everyone as viewer
	return "viewer", true
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

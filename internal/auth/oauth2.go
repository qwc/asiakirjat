package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
	"golang.org/x/oauth2"
)

// OAuth2Authenticator handles OAuth2/OIDC authentication flows.
type OAuth2Authenticator struct {
	cfg           config.OAuth2Config
	oauthConfig   *oauth2.Config
	userInfoURL   string
	users         store.UserStore
	access        store.ProjectAccessStore
	groupMappings store.AuthGroupMappingStore
	globalAccess  store.GlobalAccessStore
	logger        *slog.Logger

	// CSRF state storage (in-memory, keyed by state token)
	mu     sync.Mutex
	states map[string]bool
}

// NewOAuth2Authenticator creates a new OAuth2 authenticator.
func NewOAuth2Authenticator(cfg config.OAuth2Config, users store.UserStore, logger *slog.Logger) *OAuth2Authenticator {
	scopes := strings.Fields(strings.ReplaceAll(cfg.Scopes, ",", " "))

	return &OAuth2Authenticator{
		cfg: cfg,
		oauthConfig: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  cfg.AuthURL,
				TokenURL: cfg.TokenURL,
			},
			RedirectURL: cfg.RedirectURL,
			Scopes:      scopes,
		},
		userInfoURL: cfg.UserInfoURL,
		users:       users,
		logger:      logger,
		states:      make(map[string]bool),
	}
}

// SetStores sets the access, group mapping, and global access stores.
// This is called after authenticator creation to avoid circular dependencies.
func (a *OAuth2Authenticator) SetStores(access store.ProjectAccessStore, groupMappings store.AuthGroupMappingStore, globalAccess store.GlobalAccessStore) {
	a.access = access
	a.groupMappings = groupMappings
	a.globalAccess = globalAccess
}

func (a *OAuth2Authenticator) Name() string {
	return "oauth2"
}

// Authenticate is not used for OAuth2 (flow is redirect-based).
func (a *OAuth2Authenticator) Authenticate(ctx context.Context, username, password string) (*database.User, error) {
	return nil, fmt.Errorf("OAuth2 does not support direct authentication")
}

// GenerateAuthURL creates a new CSRF state token and returns the OAuth2 authorization URL.
func (a *OAuth2Authenticator) GenerateAuthURL() (string, error) {
	state, err := generateState()
	if err != nil {
		return "", err
	}

	a.mu.Lock()
	a.states[state] = true
	a.mu.Unlock()

	return a.oauthConfig.AuthCodeURL(state), nil
}

// ValidateState checks if a state token is valid and consumes it.
func (a *OAuth2Authenticator) ValidateState(state string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.states[state] {
		delete(a.states, state)
		return true
	}
	return false
}

// HandleCallback exchanges the authorization code for tokens, fetches user info,
// and auto-provisions the user. Returns the provisioned user.
func (a *OAuth2Authenticator) HandleCallback(ctx context.Context, code string) (*database.User, error) {
	// Exchange authorization code for token
	token, err := a.oauthConfig.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchanging code for token: %w", err)
	}

	// Fetch user info (includes groups if configured)
	client := a.oauthConfig.Client(ctx, token)
	userInfo, groups, err := a.fetchUserInfo(client)
	if err != nil {
		return nil, fmt.Errorf("fetching user info: %w", err)
	}

	if userInfo.Username == "" && userInfo.Email == "" {
		return nil, fmt.Errorf("no username or email in user info response")
	}

	// Use preferred_username or derive from email
	username := userInfo.Username
	if username == "" {
		parts := strings.SplitN(userInfo.Email, "@", 2)
		username = parts[0]
	}

	// Determine role from group membership (if configured)
	a.logger.Debug("OAuth2 user groups", "username", username, "groups", groups)
	role, allowed := a.mapGroupsToRole(groups)
	if !allowed {
		a.logger.Debug("OAuth2 user not in any allowed group", "username", username, "admin_group", a.cfg.AdminGroup, "editor_group", a.cfg.EditorGroup, "viewer_group", a.cfg.ViewerGroup)
		return nil, fmt.Errorf("user not in any allowed group")
	}
	a.logger.Debug("OAuth2 role resolved", "username", username, "role", role)

	// Auto-provision or update user
	user, err := a.provisionUser(ctx, username, userInfo.Email, role)
	if err != nil {
		return nil, fmt.Errorf("provisioning user: %w", err)
	}

	// Sync project access based on group mappings
	if a.access != nil && a.groupMappings != nil {
		if err := a.syncProjectAccess(ctx, user, groups); err != nil {
			a.logger.Warn("syncing OAuth2 project access", "username", username, "error", err)
		}
	}

	// Sync global access based on group membership
	if a.globalAccess != nil {
		if err := a.syncGlobalAccess(ctx, user, groups); err != nil {
			a.logger.Warn("syncing OAuth2 global access", "username", username, "error", err)
		}
	}

	return user, nil
}

// UserInfo represents the user information from the OAuth2 provider.
type UserInfo struct {
	Sub      string `json:"sub"`
	Username string `json:"preferred_username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
}

func (a *OAuth2Authenticator) fetchUserInfo(client *http.Client) (*UserInfo, []string, error) {
	resp, err := client.Get(a.userInfoURL)
	if err != nil {
		return nil, nil, fmt.Errorf("requesting user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("user info endpoint returned %d", resp.StatusCode)
	}

	// Decode into a generic map first to extract groups
	var rawInfo map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rawInfo); err != nil {
		return nil, nil, fmt.Errorf("decoding user info: %w", err)
	}

	// Extract standard user info fields
	info := &UserInfo{
		Sub:      getStringField(rawInfo, "sub"),
		Username: getStringField(rawInfo, "preferred_username"),
		Email:    getStringField(rawInfo, "email"),
		Name:     getStringField(rawInfo, "name"),
	}

	// Extract groups from the configured claim
	var groups []string
	if a.cfg.GroupsClaim != "" {
		groups = extractGroups(rawInfo, a.cfg.GroupsClaim)
	}

	return info, groups, nil
}

// getStringField safely extracts a string field from a map
func getStringField(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// extractGroups extracts group names from the userinfo response
func extractGroups(rawInfo map[string]any, claimName string) []string {
	var groups []string

	// Handle nested claims (e.g., "resource_access.app.roles")
	parts := strings.Split(claimName, ".")
	var current any = rawInfo
	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
			current = m[part]
		} else {
			return groups
		}
	}

	// Handle different group claim formats
	switch v := current.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				groups = append(groups, s)
			}
		}
	case []string:
		groups = v
	case string:
		// Some providers return space-separated groups
		groups = strings.Fields(v)
	}

	return groups
}

// mapGroupsToRole determines a user's role based on OAuth2 group membership.
// Returns the role and whether the user is allowed.
func (a *OAuth2Authenticator) mapGroupsToRole(groups []string) (string, bool) {
	// If no group configuration, allow all users as viewers (backward compatible)
	if a.cfg.AdminGroup == "" && a.cfg.EditorGroup == "" && a.cfg.ViewerGroup == "" {
		return "viewer", true
	}

	// Check for admin group first (highest priority)
	for _, group := range groups {
		if a.cfg.AdminGroup != "" && strings.EqualFold(group, a.cfg.AdminGroup) {
			return "admin", true
		}
	}
	// Check for editor group
	for _, group := range groups {
		if a.cfg.EditorGroup != "" && strings.EqualFold(group, a.cfg.EditorGroup) {
			return "editor", true
		}
	}
	// Check for viewer group
	for _, group := range groups {
		if a.cfg.ViewerGroup != "" && strings.EqualFold(group, a.cfg.ViewerGroup) {
			return "viewer", true
		}
	}

	// If viewerGroup is set, user must be in one of the groups to be allowed
	if a.cfg.ViewerGroup != "" {
		return "", false
	}

	// Backward compatible: if no viewerGroup configured, allow everyone as viewer
	return "viewer", true
}

func (a *OAuth2Authenticator) provisionUser(ctx context.Context, username, email, role string) (*database.User, error) {
	existing, err := a.users.GetByUsername(ctx, username)
	if err == nil && existing != nil {
		// Update role and email if changed
		if existing.Role != role || (existing.Email != email && email != "") {
			existing.Role = role
			if email != "" {
				existing.Email = email
			}
			if err := a.users.Update(ctx, existing); err != nil {
				a.logger.Warn("updating OAuth2 user", "username", username, "error", err)
			}
		}
		return existing, nil
	}

	// Create new user
	user := &database.User{
		Username:   username,
		Email:      email,
		AuthSource: "oauth2",
		Role:       role,
	}
	if err := a.users.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("creating OAuth2 user: %w", err)
	}

	a.logger.Info("auto-provisioned OAuth2 user", "username", username, "role", role)
	return user, nil
}

// syncProjectAccess synchronizes project access for a user based on their OAuth2 group membership.
func (a *OAuth2Authenticator) syncProjectAccess(ctx context.Context, user *database.User, groups []string) error {
	// Get all OAuth2 group mappings from the database
	mappings, err := a.groupMappings.ListBySource(ctx, "oauth2")
	if err != nil {
		return fmt.Errorf("listing OAuth2 group mappings: %w", err)
	}

	if len(mappings) == 0 {
		a.logger.Debug("no OAuth2 group mappings configured, skipping project access sync", "username", user.Username)
		return nil
	}

	a.logger.Debug("syncing OAuth2 project access", "username", user.Username, "mappings_count", len(mappings), "user_groups", groups)

	// Build a set of user's groups for fast lookup (case-insensitive)
	userGroups := make(map[string]bool)
	for _, g := range groups {
		userGroups[strings.ToLower(g)] = true
	}

	// Track which projects the user should have access to via OAuth2
	grantedProjects := make(map[int64]string) // project_id -> highest role

	for _, mapping := range mappings {
		matched := userGroups[strings.ToLower(mapping.GroupIdentifier)]
		a.logger.Debug("OAuth2 group mapping check", "username", user.Username, "group", mapping.GroupIdentifier, "project_id", mapping.ProjectID, "role", mapping.Role, "matched", matched)
		if matched {
			currentRole := grantedProjects[mapping.ProjectID]
			if roleHigher(mapping.Role, currentRole) {
				grantedProjects[mapping.ProjectID] = mapping.Role
			}
		}
	}

	// Get existing OAuth2-sourced access for this user
	existingAccess, err := a.access.ListByUserAndSource(ctx, user.ID, "oauth2")
	if err != nil {
		return fmt.Errorf("listing existing OAuth2 access: %w", err)
	}

	existingProjects := make(map[int64]string)
	for _, access := range existingAccess {
		existingProjects[access.ProjectID] = access.Role
	}

	// Grant new or update existing access
	for projectID, role := range grantedProjects {
		if existingRole, exists := existingProjects[projectID]; !exists || existingRole != role {
			a.logger.Debug("granting OAuth2 project access", "username", user.Username, "project_id", projectID, "role", role)
			access := &database.ProjectAccess{
				ProjectID: projectID,
				UserID:    user.ID,
				Role:      role,
				Source:    "oauth2",
			}
			if err := a.access.Grant(ctx, access); err != nil {
				a.logger.Warn("granting OAuth2 project access", "project_id", projectID, "error", err)
			}
		}
	}

	// Revoke access for projects no longer granted by OAuth2
	for projectID := range existingProjects {
		if _, shouldHave := grantedProjects[projectID]; !shouldHave {
			a.logger.Debug("revoking OAuth2 project access", "username", user.Username, "project_id", projectID)
			if err := a.access.RevokeBySource(ctx, projectID, user.ID, "oauth2"); err != nil {
				a.logger.Warn("revoking OAuth2 project access", "project_id", projectID, "error", err)
			}
		}
	}

	a.logger.Debug("OAuth2 project access sync complete", "username", user.Username, "granted_projects", len(grantedProjects))
	return nil
}

// syncGlobalAccess resolves the user's OAuth2 group membership against global access rules
// and upserts or deletes the user's global access grant accordingly.
func (a *OAuth2Authenticator) syncGlobalAccess(ctx context.Context, user *database.User, groups []string) error {
	rules, err := a.globalAccess.ListRules(ctx)
	if err != nil {
		return fmt.Errorf("listing global access rules: %w", err)
	}

	a.logger.Debug("syncing OAuth2 global access", "username", user.Username, "rules_count", len(rules), "user_groups", groups)

	// Build a set of user's groups for fast lookup (case-insensitive)
	userGroups := make(map[string]bool)
	for _, g := range groups {
		userGroups[strings.ToLower(g)] = true
	}

	// Find the highest role from matching OAuth2 group rules
	var bestRole string
	for _, rule := range rules {
		if rule.SubjectType != "oauth2_group" {
			continue
		}
		matched := userGroups[strings.ToLower(rule.SubjectIdentifier)]
		a.logger.Debug("global access rule check", "username", user.Username, "rule_subject", rule.SubjectIdentifier, "rule_role", rule.Role, "matched", matched)
		if matched {
			if roleHigher(rule.Role, bestRole) {
				bestRole = rule.Role
			}
		}
	}

	if bestRole != "" {
		a.logger.Debug("granting global access", "username", user.Username, "role", bestRole, "source", "oauth2")
		grant := &database.GlobalAccessGrant{
			UserID: user.ID,
			Role:   bestRole,
			Source: "oauth2",
		}
		if err := a.globalAccess.UpsertGrant(ctx, grant); err != nil {
			return fmt.Errorf("upserting global access grant: %w", err)
		}
	} else {
		a.logger.Debug("no matching global access rules, removing OAuth2 grants", "username", user.Username)
		if err := a.globalAccess.DeleteGrantsBySource(ctx, user.ID, "oauth2"); err != nil {
			return fmt.Errorf("deleting global access grants: %w", err)
		}
	}

	return nil
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ValidateOAuth2Config checks that required OAuth2 config fields are set.
func ValidateOAuth2Config(cfg config.OAuth2Config) error {
	if cfg.ClientID == "" {
		return fmt.Errorf("OAuth2 client ID is required")
	}
	if cfg.ClientSecret == "" {
		return fmt.Errorf("OAuth2 client secret is required")
	}
	if cfg.AuthURL == "" {
		return fmt.Errorf("OAuth2 auth URL is required")
	}
	if cfg.TokenURL == "" {
		return fmt.Errorf("OAuth2 token URL is required")
	}
	if cfg.UserInfoURL == "" {
		return fmt.Errorf("OAuth2 user info URL is required")
	}
	if cfg.RedirectURL == "" {
		return fmt.Errorf("OAuth2 redirect URL is required")
	}
	return nil
}

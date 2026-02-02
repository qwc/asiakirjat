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
	oauthConfig *oauth2.Config
	userInfoURL string
	users       store.UserStore
	logger      *slog.Logger

	// CSRF state storage (in-memory, keyed by state token)
	mu     sync.Mutex
	states map[string]bool
}

// NewOAuth2Authenticator creates a new OAuth2 authenticator.
func NewOAuth2Authenticator(cfg config.OAuth2Config, users store.UserStore, logger *slog.Logger) *OAuth2Authenticator {
	scopes := strings.Fields(strings.ReplaceAll(cfg.Scopes, ",", " "))

	return &OAuth2Authenticator{
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

	// Fetch user info
	client := a.oauthConfig.Client(ctx, token)
	userInfo, err := a.fetchUserInfo(client)
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

	// Auto-provision or update user
	user, err := a.provisionUser(ctx, username, userInfo.Email)
	if err != nil {
		return nil, fmt.Errorf("provisioning user: %w", err)
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

func (a *OAuth2Authenticator) fetchUserInfo(client *http.Client) (*UserInfo, error) {
	resp, err := client.Get(a.userInfoURL)
	if err != nil {
		return nil, fmt.Errorf("requesting user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info endpoint returned %d", resp.StatusCode)
	}

	var info UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding user info: %w", err)
	}

	return &info, nil
}

func (a *OAuth2Authenticator) provisionUser(ctx context.Context, username, email string) (*database.User, error) {
	existing, err := a.users.GetByUsername(ctx, username)
	if err == nil && existing != nil {
		// Update email if changed
		if existing.Email != email && email != "" {
			existing.Email = email
			if err := a.users.Update(ctx, existing); err != nil {
				a.logger.Warn("updating OAuth2 user email", "username", username, "error", err)
			}
		}
		return existing, nil
	}

	// Create new user with default viewer role
	user := &database.User{
		Username:   username,
		Email:      email,
		AuthSource: "oauth2",
		Role:       "viewer",
	}
	if err := a.users.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("creating OAuth2 user: %w", err)
	}

	a.logger.Info("auto-provisioned OAuth2 user", "username", username)
	return user, nil
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

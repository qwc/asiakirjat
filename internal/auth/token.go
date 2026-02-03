package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
)

type TokenAuthenticator struct {
	tokens store.TokenStore
	users  store.UserStore
}

func NewTokenAuthenticator(tokens store.TokenStore, users store.UserStore) *TokenAuthenticator {
	return &TokenAuthenticator{tokens: tokens, users: users}
}

func (a *TokenAuthenticator) AuthenticateRequest(r *http.Request) *database.User {
	user, _ := a.authenticateRequestInternal(r)
	return user
}

// AuthenticateRequestForProject authenticates the request and validates that
// the token is valid for the specified project. Returns nil if the token is
// not valid or is scoped to a different project.
func (a *TokenAuthenticator) AuthenticateRequestForProject(r *http.Request, projectID int64) *database.User {
	user, token := a.authenticateRequestInternal(r)
	if user == nil || token == nil {
		return nil
	}

	// Check project scope: if token has a project_id, it must match
	if token.ProjectID != nil && *token.ProjectID != projectID {
		return nil
	}

	return user
}

func (a *TokenAuthenticator) authenticateRequestInternal(r *http.Request) (*database.User, *database.APIToken) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return nil, nil
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, nil
	}

	rawToken := strings.TrimSpace(parts[1])
	hash := HashToken(rawToken)

	token, err := a.tokens.GetByHash(r.Context(), hash)
	if err != nil {
		return nil, nil
	}

	// Check expiry
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return nil, nil
	}

	user, err := a.users.GetByID(r.Context(), token.UserID)
	if err != nil {
		return nil, nil
	}

	return user, token
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

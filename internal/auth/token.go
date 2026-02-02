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
	header := r.Header.Get("Authorization")
	if header == "" {
		return nil
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil
	}

	rawToken := strings.TrimSpace(parts[1])
	hash := HashToken(rawToken)

	token, err := a.tokens.GetByHash(r.Context(), hash)
	if err != nil {
		return nil
	}

	// Check expiry
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return nil
	}

	user, err := a.users.GetByID(r.Context(), token.UserID)
	if err != nil {
		return nil
	}

	return user
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

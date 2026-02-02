package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
)

type SessionManager struct {
	store      store.SessionStore
	userStore  store.UserStore
	cookieName string
	maxAge     int
	secure     bool
}

func NewSessionManager(sessionStore store.SessionStore, userStore store.UserStore, cookieName string, maxAge int, secure bool) *SessionManager {
	return &SessionManager{
		store:      sessionStore,
		userStore:  userStore,
		cookieName: cookieName,
		maxAge:     maxAge,
		secure:     secure,
	}
}

func (sm *SessionManager) CreateSession(ctx context.Context, w http.ResponseWriter, userID int64) error {
	token, err := GenerateToken(32)
	if err != nil {
		return fmt.Errorf("generating session token: %w", err)
	}

	session := &database.Session{
		ID:        token,
		UserID:    userID,
		ExpiresAt: time.Now().Add(time.Duration(sm.maxAge) * time.Second),
	}

	if err := sm.store.Create(ctx, session); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sm.cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   sm.maxAge,
		HttpOnly: true,
		Secure:   sm.secure,
		SameSite: http.SameSiteLaxMode,
	})

	return nil
}

func (sm *SessionManager) GetUserFromRequest(r *http.Request) *database.User {
	cookie, err := r.Cookie(sm.cookieName)
	if err != nil {
		return nil
	}

	session, err := sm.store.GetByID(r.Context(), cookie.Value)
	if err != nil {
		return nil
	}

	if session.ExpiresAt.Before(time.Now()) {
		sm.store.Delete(r.Context(), session.ID)
		return nil
	}

	user, err := sm.userStore.GetByID(r.Context(), session.UserID)
	if err != nil {
		return nil
	}

	return user
}

func (sm *SessionManager) DestroySession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sm.cookieName)
	if err != nil {
		return
	}

	sm.store.Delete(r.Context(), cookie.Value)

	http.SetCookie(w, &http.Cookie{
		Name:     sm.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   sm.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func GenerateToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

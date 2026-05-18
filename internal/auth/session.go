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
	// csrfSecret is the HMAC key for deriving per-session CSRF tokens.
	// 32 random bytes generated at startup; held in memory. On restart the
	// secret rotates and outstanding form submissions fail (the user
	// refreshes and continues), which is the same UX as session timeout.
	csrfSecret []byte
}

// NewSessionManager constructs a manager. csrfSecret must be 32 bytes from
// a CSPRNG (auth.GenerateCSRFSecret). Passing nil disables CSRF protection,
// which only makes sense in tests that don't exercise form POSTs.
func NewSessionManager(sessionStore store.SessionStore, userStore store.UserStore, cookieName string, maxAge int, secure bool, csrfSecret []byte) *SessionManager {
	return &SessionManager{
		store:      sessionStore,
		userStore:  userStore,
		cookieName: cookieName,
		maxAge:     maxAge,
		secure:     secure,
		csrfSecret: csrfSecret,
	}
}

// CSRFToken returns the CSRF token for the request's session, or "" if
// there is no session cookie (anonymous requests). The empty value is a
// signal to templates — embedding it in a hidden input renders an empty
// value that will fail VerifyCSRF, which is the correct outcome.
func (sm *SessionManager) CSRFToken(r *http.Request) string {
	cookie, err := r.Cookie(sm.cookieName)
	if err != nil {
		return ""
	}
	return ComputeCSRFToken(sm.csrfSecret, cookie.Value)
}

// VerifyCSRF returns true when presented matches the expected CSRF token
// for the request's session. Anonymous requests, sessions with no cookie,
// and empty presented values all return false.
func (sm *SessionManager) VerifyCSRF(r *http.Request, presented string) bool {
	cookie, err := r.Cookie(sm.cookieName)
	if err != nil {
		return false
	}
	return ValidateCSRFToken(sm.csrfSecret, cookie.Value, presented)
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

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/testutil"

	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
)

func setupSessionTest(t *testing.T) (*SessionManager, *sqlstore.UserStore, *sqlstore.SessionStore, *database.User) {
	t.Helper()
	db := testutil.NewTestDB(t)

	userStore := sqlstore.NewUserStore(db)
	sessionStore := sqlstore.NewSessionStore(db)

	sm := NewSessionManager(sessionStore, userStore, "test_session", 3600, false)

	ctx := context.Background()
	pwd := "hashed"
	user := &database.User{
		Username:   "testuser",
		Password:   &pwd,
		AuthSource: "builtin",
		Role:       "viewer",
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatal(err)
	}

	return sm, userStore, sessionStore, user
}

func TestCreateSession(t *testing.T) {
	sm, _, _, user := setupSessionTest(t)

	w := httptest.NewRecorder()
	ctx := context.Background()

	if err := sm.CreateSession(ctx, w, user.ID); err != nil {
		t.Fatal(err)
	}

	resp := w.Result()
	cookies := resp.Cookies()

	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != "test_session" {
		t.Errorf("expected cookie name 'test_session', got %q", cookie.Name)
	}
	if !cookie.HttpOnly {
		t.Error("expected HttpOnly cookie")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Error("expected SameSite=Lax")
	}
	if cookie.Value == "" {
		t.Error("expected non-empty cookie value")
	}
}

func TestGetUserFromRequest_ValidSession(t *testing.T) {
	sm, _, sessionStore, user := setupSessionTest(t)
	ctx := context.Background()

	// Create a session directly in the store
	session := &database.Session{
		ID:        "valid-session-token",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	if err := sessionStore.Create(ctx, session); err != nil {
		t.Fatal(err)
	}

	// Make request with cookie
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test_session",
		Value: "valid-session-token",
	})

	got := sm.GetUserFromRequest(req)
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %q", got.Username)
	}
}

func TestGetUserFromRequest_ExpiredSession(t *testing.T) {
	sm, _, sessionStore, user := setupSessionTest(t)
	ctx := context.Background()

	// Create an expired session
	session := &database.Session{
		ID:        "expired-session-token",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if err := sessionStore.Create(ctx, session); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test_session",
		Value: "expired-session-token",
	})

	got := sm.GetUserFromRequest(req)
	if got != nil {
		t.Error("expected nil user for expired session")
	}
}

func TestGetUserFromRequest_NoCookie(t *testing.T) {
	sm, _, _, _ := setupSessionTest(t)

	req := httptest.NewRequest("GET", "/", nil)

	got := sm.GetUserFromRequest(req)
	if got != nil {
		t.Error("expected nil user when no cookie present")
	}
}

func TestGetUserFromRequest_InvalidToken(t *testing.T) {
	sm, _, _, _ := setupSessionTest(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test_session",
		Value: "nonexistent-session-token",
	})

	got := sm.GetUserFromRequest(req)
	if got != nil {
		t.Error("expected nil user for invalid session token")
	}
}

func TestDestroySession(t *testing.T) {
	sm, _, sessionStore, user := setupSessionTest(t)
	ctx := context.Background()

	// Create a session
	session := &database.Session{
		ID:        "destroy-me",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	sessionStore.Create(ctx, session)

	// Build a request with the cookie
	req := httptest.NewRequest("GET", "/logout", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test_session",
		Value: "destroy-me",
	})
	w := httptest.NewRecorder()

	sm.DestroySession(w, req)

	// Cookie should be cleared
	resp := w.Result()
	cookies := resp.Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "test_session" {
			found = true
			if c.MaxAge != -1 {
				t.Errorf("expected MaxAge -1 to clear cookie, got %d", c.MaxAge)
			}
		}
	}
	if !found {
		t.Error("expected clearing cookie to be set")
	}

	// Session should be gone from store
	_, err := sessionStore.GetByID(ctx, "destroy-me")
	if err == nil {
		t.Error("expected session to be deleted from store")
	}
}

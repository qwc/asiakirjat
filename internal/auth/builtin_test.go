package auth

import (
	"context"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("testpassword")
	if err != nil {
		t.Fatal(err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("testpassword")); err != nil {
		t.Error("hash should match original password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrongpassword")); err == nil {
		t.Error("hash should not match wrong password")
	}
}

func TestGenerateToken(t *testing.T) {
	token1, err := GenerateToken(32)
	if err != nil {
		t.Fatal(err)
	}

	if len(token1) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("expected token length 64, got %d", len(token1))
	}

	token2, err := GenerateToken(32)
	if err != nil {
		t.Fatal(err)
	}

	if token1 == token2 {
		t.Error("tokens should be unique")
	}
}

func TestHashToken(t *testing.T) {
	hash := HashToken("test-token")
	if hash == "" {
		t.Error("hash should not be empty")
	}

	// Same input should produce same hash
	hash2 := HashToken("test-token")
	if hash != hash2 {
		t.Error("same input should produce same hash")
	}

	// Different input should produce different hash
	hash3 := HashToken("other-token")
	if hash == hash3 {
		t.Error("different input should produce different hash")
	}
}

func TestContextUser(t *testing.T) {
	ctx := context.Background()

	// No user in context
	user := UserFromContext(ctx)
	if user != nil {
		t.Error("expected nil user from empty context")
	}

	// User in context
	u := &database.User{ID: 1, Username: "test"}
	ctx2 := ContextWithUser(ctx, u)
	got := UserFromContext(ctx2)
	if got == nil {
		t.Fatal("expected user in context")
	}
	if got.Username != "test" {
		t.Errorf("expected username 'test', got %q", got.Username)
	}
}

func setupBuiltinAuth(t *testing.T) (*BuiltinAuthenticator, *sqlstore.UserStore) {
	t.Helper()
	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	auth := NewBuiltinAuthenticator(userStore)
	return auth, userStore
}

func TestBuiltinAuthenticateSuccess(t *testing.T) {
	auth, userStore := setupBuiltinAuth(t)
	ctx := context.Background()

	hash, _ := HashPassword("secret123")
	user := &database.User{
		Username:   "alice",
		Password:   &hash,
		AuthSource: "builtin",
		Role:       "editor",
	}
	userStore.Create(ctx, user)

	got, err := auth.Authenticate(ctx, "alice", "secret123")
	if err != nil {
		t.Fatalf("expected successful auth, got error: %v", err)
	}
	if got.Username != "alice" {
		t.Errorf("expected username 'alice', got %q", got.Username)
	}
}

func TestBuiltinAuthenticateWrongPassword(t *testing.T) {
	auth, userStore := setupBuiltinAuth(t)
	ctx := context.Background()

	hash, _ := HashPassword("secret123")
	user := &database.User{
		Username:   "alice",
		Password:   &hash,
		AuthSource: "builtin",
		Role:       "editor",
	}
	userStore.Create(ctx, user)

	_, err := auth.Authenticate(ctx, "alice", "wrongpassword")
	if err == nil {
		t.Error("expected error for wrong password")
	}
}

func TestBuiltinAuthenticateUserNotFound(t *testing.T) {
	auth, _ := setupBuiltinAuth(t)
	ctx := context.Background()

	_, err := auth.Authenticate(ctx, "nobody", "password")
	if err == nil {
		t.Error("expected error for nonexistent user")
	}
}

func TestBuiltinAuthenticateWrongAuthSource(t *testing.T) {
	auth, userStore := setupBuiltinAuth(t)
	ctx := context.Background()

	user := &database.User{
		Username:   "ldapuser",
		AuthSource: "ldap",
		Role:       "viewer",
	}
	userStore.Create(ctx, user)

	_, err := auth.Authenticate(ctx, "ldapuser", "anything")
	if err == nil {
		t.Error("expected error for non-builtin auth source")
	}
}

func TestBuiltinAuthenticateNullPassword(t *testing.T) {
	auth, userStore := setupBuiltinAuth(t)
	ctx := context.Background()

	user := &database.User{
		Username:   "nopwd",
		Password:   nil,
		AuthSource: "builtin",
		Role:       "viewer",
	}
	userStore.Create(ctx, user)

	_, err := auth.Authenticate(ctx, "nopwd", "anything")
	if err == nil {
		t.Error("expected error for user with no password")
	}
}

func TestBuiltinAuthenticateRobotRejected(t *testing.T) {
	auth, userStore := setupBuiltinAuth(t)
	ctx := context.Background()

	hash, _ := HashPassword("robotpwd")
	user := &database.User{
		Username:   "ci-bot",
		Password:   &hash,
		AuthSource: "builtin",
		Role:       "editor",
		IsRobot:    true,
	}
	userStore.Create(ctx, user)

	_, err := auth.Authenticate(ctx, "ci-bot", "robotpwd")
	if err == nil {
		t.Error("expected error for robot user")
	}
}

func TestBuiltinAuthenticatorName(t *testing.T) {
	auth, _ := setupBuiltinAuth(t)
	if auth.Name() != "builtin" {
		t.Errorf("expected name 'builtin', got %q", auth.Name())
	}
}

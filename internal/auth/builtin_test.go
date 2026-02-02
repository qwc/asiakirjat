package auth

import (
	"context"
	"testing"

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
}

package auth

import (
	"context"
	"fmt"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
	"golang.org/x/crypto/bcrypt"
)

type BuiltinAuthenticator struct {
	users store.UserStore
}

func NewBuiltinAuthenticator(users store.UserStore) *BuiltinAuthenticator {
	return &BuiltinAuthenticator{users: users}
}

func (a *BuiltinAuthenticator) Name() string {
	return "builtin"
}

func (a *BuiltinAuthenticator) Authenticate(ctx context.Context, username, password string) (*database.User, error) {
	user, err := a.users.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	if user.AuthSource != "builtin" {
		return nil, fmt.Errorf("user does not use builtin auth")
	}

	if user.Password == nil {
		return nil, fmt.Errorf("user has no password")
	}

	if user.IsRobot {
		return nil, fmt.Errorf("robot users cannot log in via web")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.Password), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid password")
	}

	return user, nil
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

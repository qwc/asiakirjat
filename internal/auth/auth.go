package auth

import (
	"context"

	"github.com/qwc/asiakirjat/internal/database"
)

type Authenticator interface {
	Name() string
	Authenticate(ctx context.Context, username, password string) (*database.User, error)
}

type contextKey string

const userContextKey contextKey = "user"

func UserFromContext(ctx context.Context) *database.User {
	user, _ := ctx.Value(userContextKey).(*database.User)
	return user
}

func ContextWithUser(ctx context.Context, user *database.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

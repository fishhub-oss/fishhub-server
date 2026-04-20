package auth

import (
	"context"
	"errors"
)

var ErrUserNotFound = errors.New("user not found")

type UserStore interface {
	Upsert(ctx context.Context, email, provider, providerSub string) (User, error)
	FindByID(ctx context.Context, id string) (User, error)
}

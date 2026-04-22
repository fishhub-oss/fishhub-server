package auth

import (
	"context"
	"errors"
	"time"
)

var ErrUserNotFound  = errors.New("user not found")
var ErrTokenNotFound = errors.New("refresh token not found")
var ErrTokenExpired  = errors.New("refresh token expired")
var ErrTokenRevoked  = errors.New("refresh token revoked")

type UserStore interface {
	Upsert(ctx context.Context, email, provider, providerSub string) (User, error)
	FindByID(ctx context.Context, id string) (User, error)
}

type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

type RefreshTokenStore interface {
	Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (RefreshToken, error)
	FindByHash(ctx context.Context, tokenHash string) (RefreshToken, error)
	Revoke(ctx context.Context, id string) error
}

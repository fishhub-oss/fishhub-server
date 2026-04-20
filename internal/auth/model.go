package auth

import (
	"context"
	"time"
)

type User struct {
	ID          string
	Email       string
	Provider    string
	ProviderSub string
	CreatedAt   time.Time
}

type Claims struct {
	UserID string
}

type contextKey string

const claimsContextKey contextKey = "claims"

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsContextKey).(Claims)
	return c, ok
}

func ContextWithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey, c)
}

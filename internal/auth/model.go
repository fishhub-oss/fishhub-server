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

// MustClaimsFromContext returns the Claims stored by SessionAuthenticator middleware.
// It panics if claims are absent — this indicates a misconfigured route (missing middleware).
func MustClaimsFromContext(ctx context.Context) Claims {
	c, ok := ctx.Value(claimsContextKey).(Claims)
	if !ok {
		panic("auth: claims missing from context — is SessionAuthenticator middleware applied?")
	}
	return c
}

func ContextWithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey, c)
}

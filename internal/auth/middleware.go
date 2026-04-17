package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/fishhub-oss/fishhub-server/internal/store"
)

type contextKey string

const deviceContextKey contextKey = "device"

func Authenticator(devices store.DeviceStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				http.Error(w, "missing or malformed authorization header", http.StatusUnauthorized)
				return
			}

			info, err := devices.LookupByToken(r.Context(), token)
			if errors.Is(err, store.ErrTokenNotFound) {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			if err != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			ctx := context.WithValue(r.Context(), deviceContextKey, info)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func DeviceFromContext(ctx context.Context) (store.DeviceInfo, bool) {
	info, ok := ctx.Value(deviceContextKey).(store.DeviceInfo)
	return info, ok
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

package platform

import (
	"context"
	"net/http"
	"strings"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/go-chi/render"
)

func DeviceAuthenticator(devices sensors.DeviceStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				http.Error(w, "missing or malformed authorization header", http.StatusUnauthorized)
				return
			}

			info, err := devices.LookupByToken(r.Context(), token)
			if err != nil {
				if strings.Contains(err.Error(), sensors.ErrTokenNotFound.Error()) {
					http.Error(w, "invalid token", http.StatusUnauthorized)
					return
				}
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			ctx := context.WithValue(r.Context(), sensors.DeviceContextKey, info)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func SessionAuthenticator(svc auth.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				if cookie, err := r.Cookie("session"); err == nil {
					token = cookie.Value
				}
			}
			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			userID, err := svc.ValidateSessionJWT(token)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			ctx := auth.ContextWithClaims(r.Context(), auth.Claims{UserID: userID})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type HealthResponse struct {
	Status string `json:"status"`
}

func Health(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, HealthResponse{Status: "ok"})
}

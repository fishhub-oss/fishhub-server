package platform

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/golang-jwt/jwt/v5"
)

// RequestLogger logs method, path, status, and duration for every request.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				status := ww.Status()
				level := slog.LevelInfo
				if status >= 500 {
					level = slog.LevelError
				}
				logger.Log(r.Context(), level, "request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", status,
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

// DeviceAuthenticator validates a device JWT from the Authorization header.
// It extracts sub (device_id) and user_id claims and stores DeviceInfo in the request context.
// Deprecated: LookupByToken / DeviceStore path removed — see cleanup issue #46.
func DeviceAuthenticator(signer devicejwt.Signer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				slog.Warn("device auth failure", "reason", "missing bearer token", "path", r.URL.Path)
				http.Error(w, "missing or malformed authorization header", http.StatusUnauthorized)
				return
			}

			pub := signer.PublicKey()
			if pub == nil {
				slog.Warn("device auth failure", "reason", "signer not configured", "path", r.URL.Path)
				http.Error(w, "device auth not configured", http.StatusUnauthorized)
				return
			}

			token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return pub, nil
			}, jwt.WithValidMethods([]string{"RS256"}))
			if err != nil || !token.Valid {
				slog.Warn("device auth failure", "reason", "invalid token", "path", r.URL.Path, "error", err)
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				slog.Warn("device auth failure", "reason", "invalid claims type", "path", r.URL.Path)
				http.Error(w, "invalid token claims", http.StatusUnauthorized)
				return
			}

			deviceID, _ := claims["sub"].(string)
			userID, _ := claims["user_id"].(string)
			if deviceID == "" || userID == "" {
				slog.Warn("device auth failure", "reason", "missing claims", "path", r.URL.Path)
				http.Error(w, "invalid token claims", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), sensors.DeviceContextKey, sensors.DeviceInfo{
				DeviceID: deviceID,
				UserID:   userID,
			})
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
				slog.Warn("session auth failure", "reason", "missing token", "path", r.URL.Path)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			userID, err := svc.ValidateSessionJWT(token)
			if err != nil {
				slog.Warn("session auth failure", "reason", "invalid token", "path", r.URL.Path, "error", err)
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

package platform

import (
	"context"
	"net/http"
	"strings"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/go-chi/render"
	"github.com/golang-jwt/jwt/v5"
)

// DeviceAuthenticator validates a device JWT from the Authorization header.
// It extracts sub (device_id) and user_id claims and stores DeviceInfo in the request context.
// Deprecated: LookupByToken / DeviceStore path removed — see cleanup issue #46.
func DeviceAuthenticator(signer devicejwt.Signer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				http.Error(w, "missing or malformed authorization header", http.StatusUnauthorized)
				return
			}

			pub := signer.PublicKey()
			if pub == nil {
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
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(w, "invalid token claims", http.StatusUnauthorized)
				return
			}

			deviceID, _ := claims["sub"].(string)
			userID, _ := claims["user_id"].(string)
			if deviceID == "" || userID == "" {
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

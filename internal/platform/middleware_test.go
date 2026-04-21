package platform_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

type stubAuthService struct {
	userID string
	err    error
}

func (s *stubAuthService) VerifyAndUpsert(_ context.Context, _, _ string) (auth.User, error) {
	return auth.User{}, nil
}
func (s *stubAuthService) IssueSessionJWT(_ string) (string, error) { return "", nil }
func (s *stubAuthService) ValidateSessionJWT(_ string) (string, error) {
	return s.userID, s.err
}

type stubDeviceStore struct {
	info sensors.DeviceInfo
	err  error
}

func (s *stubDeviceStore) LookupByToken(_ context.Context, _ string) (sensors.DeviceInfo, error) {
	return s.info, s.err
}

func (s *stubDeviceStore) ListByUserID(_ context.Context, _ string) ([]sensors.Device, error) {
	return nil, nil
}

func TestSessionAuthenticator(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.ClaimsFromContext(r.Context())
		if !ok || claims.UserID == "" {
			http.Error(w, "no claims in context", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	t.Run("valid JWT via cookie passes through with claims in context", func(t *testing.T) {
		mw := platform.SessionAuthenticator(&stubAuthService{userID: "user-uuid"})
		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "valid.jwt.token"})
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("valid JWT via Bearer header passes through with claims in context", func(t *testing.T) {
		mw := platform.SessionAuthenticator(&stubAuthService{userID: "user-uuid"})
		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		req.Header.Set("Authorization", "Bearer valid.jwt.token")
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Bearer header takes precedence over cookie", func(t *testing.T) {
		// cookie has invalid token, header has valid — should pass
		mw := platform.SessionAuthenticator(&stubAuthService{userID: "user-uuid"})
		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		req.Header.Set("Authorization", "Bearer valid.jwt.token")
		req.AddCookie(&http.Cookie{Name: "session", Value: "ignored.cookie"})
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("missing cookie and no header returns 401", func(t *testing.T) {
		mw := platform.SessionAuthenticator(&stubAuthService{userID: "user-uuid"})
		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("invalid JWT returns 401", func(t *testing.T) {
		mw := platform.SessionAuthenticator(&stubAuthService{err: errors.New("invalid token")})
		req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "bad.jwt.token"})
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

func TestDeviceAuthenticator(t *testing.T) {
	validInfo := sensors.DeviceInfo{DeviceID: "device-uuid", UserID: "user-uuid"}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := sensors.DeviceFromContext(r.Context())
		if !ok {
			http.Error(w, "no device in context", http.StatusInternalServerError)
			return
		}
		if info.DeviceID != validInfo.DeviceID {
			http.Error(w, "wrong device", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	t.Run("valid token passes through with device in context", func(t *testing.T) {
		mw := platform.DeviceAuthenticator(&stubDeviceStore{info: validInfo})
		req := httptest.NewRequest(http.MethodPost, "/readings", nil)
		req.Header.Set("Authorization", "Bearer validtoken")
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("missing authorization header returns 401", func(t *testing.T) {
		mw := platform.DeviceAuthenticator(&stubDeviceStore{info: validInfo})
		req := httptest.NewRequest(http.MethodPost, "/readings", nil)
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("invalid token returns 401", func(t *testing.T) {
		mw := platform.DeviceAuthenticator(&stubDeviceStore{err: sensors.ErrTokenNotFound})
		req := httptest.NewRequest(http.MethodPost, "/readings", nil)
		req.Header.Set("Authorization", "Bearer badtoken")
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("malformed authorization header returns 401", func(t *testing.T) {
		mw := platform.DeviceAuthenticator(&stubDeviceStore{info: validInfo})
		req := httptest.NewRequest(http.MethodPost, "/readings", nil)
		req.Header.Set("Authorization", "notbearer token")
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

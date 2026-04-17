package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/store"
)

type stubDeviceStore struct {
	info store.DeviceInfo
	err  error
}

func (s *stubDeviceStore) LookupByToken(_ context.Context, _ string) (store.DeviceInfo, error) {
	return s.info, s.err
}

func TestAuthenticator(t *testing.T) {
	validInfo := store.DeviceInfo{DeviceID: "device-uuid", UserID: "user-uuid"}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := auth.DeviceFromContext(r.Context())
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
		mw := auth.Authenticator(&stubDeviceStore{info: validInfo})
		req := httptest.NewRequest(http.MethodPost, "/readings", nil)
		req.Header.Set("Authorization", "Bearer validtoken")
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("missing authorization header returns 401", func(t *testing.T) {
		mw := auth.Authenticator(&stubDeviceStore{info: validInfo})
		req := httptest.NewRequest(http.MethodPost, "/readings", nil)
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("invalid token returns 401", func(t *testing.T) {
		mw := auth.Authenticator(&stubDeviceStore{err: store.ErrTokenNotFound})
		req := httptest.NewRequest(http.MethodPost, "/readings", nil)
		req.Header.Set("Authorization", "Bearer badtoken")
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("malformed authorization header returns 401", func(t *testing.T) {
		mw := auth.Authenticator(&stubDeviceStore{info: validInfo})
		req := httptest.NewRequest(http.MethodPost, "/readings", nil)
		req.Header.Set("Authorization", "notbearer token")
		w := httptest.NewRecorder()

		mw(next).ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

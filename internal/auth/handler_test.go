package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
)

type stubAuthService struct {
	user           auth.User
	upsertErr      error
	jwtToken       string
	jwtErr         error
	validateUserID string
	validateErr    error
	refreshRaw     string
	refreshErr     error
	newRaw         string
	newJWT         string
	rotateErr      error
	revokeErr      error
}

func (s *stubAuthService) VerifyAndUpsert(_ context.Context, _, _ string) (auth.User, error) {
	return s.user, s.upsertErr
}
func (s *stubAuthService) IssueSessionJWT(_ string) (string, error) {
	return s.jwtToken, s.jwtErr
}
func (s *stubAuthService) ValidateSessionJWT(_ string) (string, error) {
	return s.validateUserID, s.validateErr
}
func (s *stubAuthService) IssueRefreshToken(_ context.Context, _ string) (string, error) {
	return s.refreshRaw, s.refreshErr
}
func (s *stubAuthService) RotateRefreshToken(_ context.Context, _ string) (string, string, error) {
	return s.newRaw, s.newJWT, s.rotateErr
}
func (s *stubAuthService) RevokeRefreshToken(_ context.Context, _ string) error {
	return s.revokeErr
}

func TestVerifyHandler(t *testing.T) {
	validUser := auth.User{ID: "user-uuid"}

	t.Run("valid request returns token and refresh_token", func(t *testing.T) {
		h := auth.NewVerifyHandler(&stubAuthService{
			user:       validUser,
			jwtToken:   "signed.jwt.token",
			refreshRaw: "raw-refresh-token",
		}, nil)
		body, _ := json.Marshal(map[string]string{"provider": "google", "id_token": "raw-id-token"})
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]string
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["token"] != "signed.jwt.token" {
			t.Errorf("token: got %q, want %q", resp["token"], "signed.jwt.token")
		}
		if resp["refresh_token"] != "raw-refresh-token" {
			t.Errorf("refresh_token: got %q, want %q", resp["refresh_token"], "raw-refresh-token")
		}
	})

	t.Run("missing id_token returns 400", func(t *testing.T) {
		h := auth.NewVerifyHandler(&stubAuthService{}, nil)
		body, _ := json.Marshal(map[string]string{"provider": "google"})
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing provider returns 400", func(t *testing.T) {
		h := auth.NewVerifyHandler(&stubAuthService{}, nil)
		body, _ := json.Marshal(map[string]string{"id_token": "tok"})
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("unsupported provider returns 422", func(t *testing.T) {
		h := auth.NewVerifyHandler(&stubAuthService{upsertErr: auth.ErrUnsupportedProvider}, nil)
		body, _ := json.Marshal(map[string]string{"provider": "github", "id_token": "tok"})
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", w.Code)
		}
	})

	t.Run("invalid id token returns 401", func(t *testing.T) {
		h := auth.NewVerifyHandler(&stubAuthService{upsertErr: auth.ErrInvalidIDToken}, nil)
		body, _ := json.Marshal(map[string]string{"provider": "google", "id_token": "bad"})
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		h := auth.NewVerifyHandler(&stubAuthService{}, nil)
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader([]byte("not json")))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestRefreshHandler(t *testing.T) {
	t.Run("valid refresh token returns new token pair", func(t *testing.T) {
		h := auth.NewRefreshHandler(&stubAuthService{
			newRaw: "new-raw-token",
			newJWT: "new.jwt.token",
		}, nil)
		body, _ := json.Marshal(map[string]string{"refresh_token": "old-raw-token"})
		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]string
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["token"] != "new.jwt.token" {
			t.Errorf("token: got %q, want %q", resp["token"], "new.jwt.token")
		}
		if resp["refresh_token"] != "new-raw-token" {
			t.Errorf("refresh_token: got %q, want %q", resp["refresh_token"], "new-raw-token")
		}
	})

	t.Run("missing refresh_token returns 400", func(t *testing.T) {
		h := auth.NewRefreshHandler(&stubAuthService{}, nil)
		body, _ := json.Marshal(map[string]string{})
		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		h := auth.NewRefreshHandler(&stubAuthService{}, nil)
		req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte("not json")))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"not found returns 401", auth.ErrTokenNotFound},
		{"expired returns 401", auth.ErrTokenExpired},
		{"revoked returns 401", auth.ErrTokenRevoked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := auth.NewRefreshHandler(&stubAuthService{rotateErr: tc.err}, nil)
			body, _ := json.Marshal(map[string]string{"refresh_token": "some-token"})
			req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", w.Code)
			}
		})
	}
}

func TestLogoutHandler(t *testing.T) {
	t.Run("clears session cookie", func(t *testing.T) {
		h := auth.NewLogoutHandler(&stubAuthService{})
		body, _ := json.Marshal(map[string]string{"refresh_token": "some-token"})
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		var found bool
		for _, c := range w.Result().Cookies() {
			if c.Name == "session" && c.MaxAge < 0 {
				found = true
			}
		}
		if !found {
			t.Error("expected session cookie with MaxAge < 0 to clear it")
		}
	})

	t.Run("works without body", func(t *testing.T) {
		h := auth.NewLogoutHandler(&stubAuthService{})
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

func TestLogout(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	w := httptest.NewRecorder()
	auth.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" && c.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected session cookie with MaxAge < 0 to clear it")
	}
}

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
	user     auth.User
	upsertErr error
	jwtToken string
	jwtErr   error
	validateUserID string
	validateErr    error
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

func TestVerifyHandler(t *testing.T) {
	validUser := auth.User{ID: "user-uuid"}

	t.Run("valid request returns 200 with token", func(t *testing.T) {
		h := &auth.VerifyHandler{Service: &stubAuthService{
			user:     validUser,
			jwtToken: "signed.jwt.token",
		}}
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
	})

	t.Run("missing id_token returns 400", func(t *testing.T) {
		h := &auth.VerifyHandler{Service: &stubAuthService{}}
		body, _ := json.Marshal(map[string]string{"provider": "google"})
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing provider returns 400", func(t *testing.T) {
		h := &auth.VerifyHandler{Service: &stubAuthService{}}
		body, _ := json.Marshal(map[string]string{"id_token": "tok"})
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("unsupported provider returns 422", func(t *testing.T) {
		h := &auth.VerifyHandler{Service: &stubAuthService{upsertErr: auth.ErrUnsupportedProvider}}
		body, _ := json.Marshal(map[string]string{"provider": "github", "id_token": "tok"})
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", w.Code)
		}
	})

	t.Run("invalid id token returns 401", func(t *testing.T) {
		h := &auth.VerifyHandler{Service: &stubAuthService{upsertErr: auth.ErrInvalidIDToken}}
		body, _ := json.Marshal(map[string]string{"provider": "google", "id_token": "bad"})
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		h := &auth.VerifyHandler{Service: &stubAuthService{}}
		req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader([]byte("not json")))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
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

	cookie := w.Result().Cookies()
	var found bool
	for _, c := range cookie {
		if c.Name == "session" && c.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected session cookie with MaxAge < 0 to clear it")
	}
}

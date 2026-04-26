package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/account"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
)

type stubAccountStore struct {
	account account.Account
	err     error
}

func (s *stubAccountStore) Upsert(_ context.Context, _, _, _ string) (account.Account, error) {
	return s.account, s.err
}

func (s *stubAccountStore) FindByUserID(_ context.Context, _ string) (account.Account, error) {
	return s.account, s.err
}

func requestWithClaims(userID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	ctx := auth.ContextWithClaims(req.Context(), auth.Claims{UserID: userID})
	return req.WithContext(ctx)
}

func TestMeHandler(t *testing.T) {
	validAccount := account.Account{
		ID:        "acct-uuid",
		UserID:    "user-uuid",
		Email:     "alice@example.com",
		Name:      "Alice",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	t.Run("returns account for authenticated user", func(t *testing.T) {
		h := &account.MeHandler{Service: &account.AccountService{Store: &stubAccountStore{account: validAccount}}}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, requestWithClaims("user-uuid"))

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]string
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["email"] != "alice@example.com" {
			t.Errorf("email: got %q, want %q", resp["email"], "alice@example.com")
		}
		if resp["name"] != "Alice" {
			t.Errorf("name: got %q, want %q", resp["name"], "Alice")
		}
	})

	t.Run("returns 401 when no claims in context", func(t *testing.T) {
		h := &account.MeHandler{Service: &account.AccountService{Store: &stubAccountStore{account: validAccount}}}
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("returns 404 when account not found", func(t *testing.T) {
		h := &account.MeHandler{Service: &account.AccountService{Store: &stubAccountStore{err: account.ErrAccountNotFound}}}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, requestWithClaims("user-uuid"))

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("returns 500 on store error", func(t *testing.T) {
		h := &account.MeHandler{Service: &account.AccountService{Store: &stubAccountStore{err: errors.New("db error")}}}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, requestWithClaims("user-uuid"))

		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
	})
}

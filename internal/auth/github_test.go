package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
)

// stubGitHubExchanger is an in-memory GitHubExchanger for unit tests.
type stubGitHubExchanger struct {
	email       string
	name        string
	providerSub string
	err         error
}

func (s *stubGitHubExchanger) Exchange(_ context.Context, _ string) (string, string, string, error) {
	return s.email, s.name, s.providerSub, s.err
}

func githubService(t *testing.T, exchanger auth.GitHubExchanger) auth.AuthService {
	t.Helper()
	svc, err := auth.NewOIDCService(context.Background(), auth.OIDCConfig{
		Providers:       map[string]string{},
		Store:           &stubUserStore{},
		RefreshStore:    &stubRefreshTokenStore{},
		Signer:          newTestSigner(t),
		JWTTTL:          time.Hour,
		GitHubExchanger: exchanger,
	})
	if err != nil {
		t.Fatalf("NewOIDCService: %v", err)
	}
	return svc
}

func TestVerifyAndUpsert_GitHub_Success(t *testing.T) {
	exchanger := &stubGitHubExchanger{
		email:       "user@example.com",
		name:        "Alice",
		providerSub: "12345",
	}
	svc := githubService(t, exchanger)

	user, err := svc.VerifyAndUpsert(context.Background(), "github", "oauth-code")
	if err != nil {
		t.Fatalf("VerifyAndUpsert: %v", err)
	}
	if user.Email != "user@example.com" {
		t.Errorf("email: got %q, want %q", user.Email, "user@example.com")
	}
	if user.Provider != "github" {
		t.Errorf("provider: got %q, want %q", user.Provider, "github")
	}
	if user.ProviderSub != "12345" {
		t.Errorf("provider_sub: got %q, want %q", user.ProviderSub, "12345")
	}
}

func TestVerifyAndUpsert_GitHub_ExchangeError(t *testing.T) {
	exchanger := &stubGitHubExchanger{err: auth.ErrInvalidIDToken}
	svc := githubService(t, exchanger)

	_, err := svc.VerifyAndUpsert(context.Background(), "github", "bad-code")
	if !errors.Is(err, auth.ErrInvalidIDToken) {
		t.Errorf("expected ErrInvalidIDToken, got %v", err)
	}
}

func TestVerifyAndUpsert_GitHub_NotConfigured(t *testing.T) {
	svc, _ := auth.NewOIDCService(context.Background(), auth.OIDCConfig{
		Providers:    map[string]string{},
		Store:        &stubUserStore{},
		RefreshStore: &stubRefreshTokenStore{},
		Signer:       newTestSigner(t),
		JWTTTL:       time.Hour,
		// GitHubExchanger intentionally nil — github not configured
	})

	_, err := svc.VerifyAndUpsert(context.Background(), "github", "some-code")
	if !errors.Is(err, auth.ErrUnsupportedProvider) {
		t.Errorf("expected ErrUnsupportedProvider, got %v", err)
	}
}

func TestVerifyAndUpsert_GitHub_ProviderConflict(t *testing.T) {
	exchanger := &stubGitHubExchanger{
		email:       "taken@example.com",
		name:        "Alice",
		providerSub: "gh-123",
	}
	svc, _ := auth.NewOIDCService(context.Background(), auth.OIDCConfig{
		Providers:       map[string]string{},
		Store:           &stubUserStore{err: auth.ErrEmailTaken},
		RefreshStore:    &stubRefreshTokenStore{},
		Signer:          newTestSigner(t),
		JWTTTL:          time.Hour,
		GitHubExchanger: exchanger,
	})

	_, err := svc.VerifyAndUpsert(context.Background(), "github", "oauth-code")
	if !errors.Is(err, auth.ErrProviderConflict) {
		t.Errorf("expected ErrProviderConflict, got %v", err)
	}
}

func TestVerifyHandler_GitHub_MissingCode(t *testing.T) {
	h := auth.NewVerifyHandler(&stubAuthService{}, nil)
	body, _ := json.Marshal(map[string]string{"provider": "github"})
	req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assertErrorCode(t, w, http.StatusBadRequest, "invalid_request")
}

func TestVerifyHandler_GitHub_Success(t *testing.T) {
	h := auth.NewVerifyHandler(&stubAuthService{
		user:       auth.User{ID: "user-uuid"},
		jwtToken:   "session.jwt",
		refreshRaw: "refresh-token",
	}, nil)
	body, _ := json.Marshal(map[string]string{"provider": "github", "code": "gh-oauth-code"})
	req := httptest.NewRequest(http.MethodPost, "/auth/verify", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["token"] != "session.jwt" {
		t.Errorf("token: got %q, want %q", resp["token"], "session.jwt")
	}
}

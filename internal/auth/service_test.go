package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
)

// jwtOnlyService builds an oidcService with no OIDC verifiers (empty providers map)
// so we can test JWT issue/validate in isolation.
func jwtOnlyService(t *testing.T, secret string, ttl time.Duration) auth.AuthService {
	t.Helper()
	svc, err := auth.NewOIDCService(context.Background(), auth.OIDCConfig{
		Providers: map[string]string{},
		Store:     &stubUserStore{},
		JWTSecret: secret,
		JWTTTL:    ttl,
	})
	if err != nil {
		t.Fatalf("NewOIDCService: %v", err)
	}
	return svc
}

func TestIssueAndValidateSessionJWT(t *testing.T) {
	svc := jwtOnlyService(t, "test-secret-32-bytes-long-enough!", time.Hour)

	token, err := svc.IssueSessionJWT("user-uuid-123")
	if err != nil {
		t.Fatalf("IssueSessionJWT: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	userID, err := svc.ValidateSessionJWT(token)
	if err != nil {
		t.Fatalf("ValidateSessionJWT: %v", err)
	}
	if userID != "user-uuid-123" {
		t.Errorf("userID: got %q, want %q", userID, "user-uuid-123")
	}
}

func TestValidateSessionJWT_Expired(t *testing.T) {
	svc := jwtOnlyService(t, "test-secret-32-bytes-long-enough!", -time.Second)

	token, err := svc.IssueSessionJWT("user-uuid-123")
	if err != nil {
		t.Fatalf("IssueSessionJWT: %v", err)
	}

	_, err = svc.ValidateSessionJWT(token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateSessionJWT_WrongSecret(t *testing.T) {
	svc1 := jwtOnlyService(t, "secret-one-32-bytes-long-enough!!", time.Hour)
	svc2 := jwtOnlyService(t, "secret-two-32-bytes-long-enough!!", time.Hour)

	token, err := svc1.IssueSessionJWT("user-uuid-123")
	if err != nil {
		t.Fatalf("IssueSessionJWT: %v", err)
	}

	_, err = svc2.ValidateSessionJWT(token)
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestVerifyAndUpsert_UnsupportedProvider(t *testing.T) {
	svc := jwtOnlyService(t, "test-secret-32-bytes-long-enough!", time.Hour)

	_, err := svc.VerifyAndUpsert(context.Background(), "github", "some-token")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

// stubUserStore satisfies UserStore for unit tests.
type stubUserStore struct {
	user auth.User
	err  error
}

func (s *stubUserStore) Upsert(_ context.Context, email, provider, sub string) (auth.User, error) {
	if s.err != nil {
		return auth.User{}, s.err
	}
	return auth.User{ID: "stub-id", Email: email, Provider: provider, ProviderSub: sub}, nil
}

func (s *stubUserStore) FindByID(_ context.Context, id string) (auth.User, error) {
	return s.user, s.err
}

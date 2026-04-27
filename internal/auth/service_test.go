package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/jwtutil"
)

func newTestSigner(t *testing.T) jwtutil.Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	s, err := jwtutil.NewRSASigner(string(pemBytes), "test-kid")
	if err != nil {
		t.Fatalf("NewRSASigner: %v", err)
	}
	return s
}

// jwtOnlyService builds an oidcService with no OIDC verifiers (empty providers map)
// so we can test JWT issue/validate in isolation.
func jwtOnlyService(t *testing.T, ttl time.Duration) auth.AuthService {
	t.Helper()
	svc, err := auth.NewOIDCService(context.Background(), auth.OIDCConfig{
		Providers:    map[string]string{},
		Store:        &stubUserStore{},
		RefreshStore: &stubRefreshTokenStore{},
		Signer:       newTestSigner(t),
		JWTTTL:       ttl,
	})
	if err != nil {
		t.Fatalf("NewOIDCService: %v", err)
	}
	return svc
}

func TestIssueAndValidateSessionJWT(t *testing.T) {
	svc := jwtOnlyService(t, time.Hour)

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
	svc := jwtOnlyService(t, -time.Second)

	token, err := svc.IssueSessionJWT("user-uuid-123")
	if err != nil {
		t.Fatalf("IssueSessionJWT: %v", err)
	}

	_, err = svc.ValidateSessionJWT(token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateSessionJWT_WrongKey(t *testing.T) {
	svc1 := jwtOnlyService(t, time.Hour)
	svc2 := jwtOnlyService(t, time.Hour)

	token, err := svc1.IssueSessionJWT("user-uuid-123")
	if err != nil {
		t.Fatalf("IssueSessionJWT: %v", err)
	}

	_, err = svc2.ValidateSessionJWT(token)
	if err == nil {
		t.Fatal("expected error for wrong key, got nil")
	}
}

func TestVerifyAndUpsert_UnsupportedProvider(t *testing.T) {
	svc := jwtOnlyService(t, time.Hour)

	_, err := svc.VerifyAndUpsert(context.Background(), "github", "some-token")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestIssueRefreshToken(t *testing.T) {
	svc := jwtOnlyService(t, time.Hour)

	raw, err := svc.IssueRefreshToken(context.Background(), "user-uuid-123")
	if err != nil {
		t.Fatalf("IssueRefreshToken: %v", err)
	}
	if raw == "" {
		t.Fatal("expected non-empty raw token")
	}
	if len(raw) != 64 {
		t.Errorf("expected 64-char hex token, got len %d", len(raw))
	}
}

func TestRotateRefreshToken(t *testing.T) {
	store := &stubRefreshTokenStore{}
	svc, err := auth.NewOIDCService(context.Background(), auth.OIDCConfig{
		Providers:    map[string]string{},
		Store:        &stubUserStore{},
		RefreshStore: store,
		Signer:       newTestSigner(t),
		JWTTTL:       time.Hour,
	})
	if err != nil {
		t.Fatalf("NewOIDCService: %v", err)
	}

	raw, err := svc.IssueRefreshToken(context.Background(), "user-uuid-123")
	if err != nil {
		t.Fatalf("IssueRefreshToken: %v", err)
	}

	newRaw, jwt, err := svc.RotateRefreshToken(context.Background(), raw)
	if err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}
	if newRaw == "" || newRaw == raw {
		t.Error("expected a different non-empty new raw token")
	}
	if jwt == "" {
		t.Error("expected non-empty session JWT")
	}
}

func TestRotateRefreshToken_Revoked(t *testing.T) {
	store := &stubRefreshTokenStore{}
	svc, _ := auth.NewOIDCService(context.Background(), auth.OIDCConfig{
		Providers:    map[string]string{},
		Store:        &stubUserStore{},
		RefreshStore: store,
		Signer:       newTestSigner(t),
		JWTTTL:       time.Hour,
	})

	raw, _ := svc.IssueRefreshToken(context.Background(), "user-uuid-123")
	svc.RevokeRefreshToken(context.Background(), raw)

	_, _, err := svc.RotateRefreshToken(context.Background(), raw)
	if err != auth.ErrTokenRevoked {
		t.Errorf("expected ErrTokenRevoked, got %v", err)
	}
}

func TestRotateRefreshToken_Expired(t *testing.T) {
	store := &stubRefreshTokenStore{ttl: -time.Second}
	svc, _ := auth.NewOIDCService(context.Background(), auth.OIDCConfig{
		Providers:    map[string]string{},
		Store:        &stubUserStore{},
		RefreshStore: store,
		Signer:       newTestSigner(t),
		JWTTTL:       time.Hour,
	})

	raw, _ := svc.IssueRefreshToken(context.Background(), "user-uuid-123")

	_, _, err := svc.RotateRefreshToken(context.Background(), raw)
	if err != auth.ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestRotateRefreshToken_NotFound(t *testing.T) {
	svc := jwtOnlyService(t, time.Hour)

	_, _, err := svc.RotateRefreshToken(context.Background(), "nonexistent-token")
	if err != auth.ErrTokenNotFound {
		t.Errorf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestRevokeRefreshToken_NotFound(t *testing.T) {
	svc := jwtOnlyService(t, time.Hour)

	err := svc.RevokeRefreshToken(context.Background(), "nonexistent-token")
	if err != auth.ErrTokenNotFound {
		t.Errorf("expected ErrTokenNotFound, got %v", err)
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

// stubRefreshTokenStore is an in-memory RefreshTokenStore for unit tests.
type stubRefreshTokenStore struct {
	tokens map[string]auth.RefreshToken // keyed by token_hash
	nextID int
	ttl    time.Duration // override TTL; zero means use the value passed to Create
}

func (s *stubRefreshTokenStore) Create(_ context.Context, userID, tokenHash string, expiresAt time.Time) (auth.RefreshToken, error) {
	if s.tokens == nil {
		s.tokens = map[string]auth.RefreshToken{}
	}
	s.nextID++
	if s.ttl != 0 {
		expiresAt = time.Now().Add(s.ttl)
	}
	rt := auth.RefreshToken{
		ID:        fmt.Sprintf("stub-%d", s.nextID),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	s.tokens[tokenHash] = rt
	return rt, nil
}

func (s *stubRefreshTokenStore) FindByHash(_ context.Context, tokenHash string) (auth.RefreshToken, error) {
	if s.tokens == nil {
		return auth.RefreshToken{}, auth.ErrTokenNotFound
	}
	rt, ok := s.tokens[tokenHash]
	if !ok {
		return auth.RefreshToken{}, auth.ErrTokenNotFound
	}
	return rt, nil
}

func (s *stubRefreshTokenStore) Revoke(_ context.Context, id string) error {
	for hash, rt := range s.tokens {
		if rt.ID == id {
			now := time.Now()
			rt.RevokedAt = &now
			s.tokens[hash] = rt
			return nil
		}
	}
	return auth.ErrTokenNotFound
}

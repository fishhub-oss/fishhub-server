package auth

// White-box tests for UserEventHandler dispatch in oidcService.
// Uses package auth directly so we can construct oidcService without going
// through the OIDC verifier path.

import (
	"context"
	"errors"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

type stubEventHandler struct {
	calls []eventCall
	err   error
}

type eventCall struct {
	userID string
	email  string
	name   string
}

func (h *stubEventHandler) OnUserVerified(_ context.Context, userID, email, name string) error {
	h.calls = append(h.calls, eventCall{userID, email, name})
	return h.err
}

// directService builds an oidcService with a pre-wired store and event handler,
// bypassing OIDC provider setup (no verifiers).
func directService(store UserStore, handler UserEventHandler) *oidcService {
	return &oidcService{
		verifiers:    map[string]*gooidc.IDTokenVerifier{},
		store:        store,
		refreshStore: &noopRefreshStore{},
		eventHandler: handler,
		jwtSecret:    []byte("secret"),
		jwtTTL:       time.Hour,
	}
}

// noopRefreshStore is a no-op RefreshTokenStore used to satisfy the field.
type noopRefreshStore struct{}

func (s *noopRefreshStore) Create(_ context.Context, _, _ string, _ time.Time) (RefreshToken, error) {
	return RefreshToken{}, nil
}
func (s *noopRefreshStore) FindByHash(_ context.Context, _ string) (RefreshToken, error) {
	return RefreshToken{}, ErrTokenNotFound
}
func (s *noopRefreshStore) Revoke(_ context.Context, _ string) error { return nil }

// stubInternalUserStore satisfies UserStore for white-box tests.
type stubInternalUserStore struct {
	user User
	err  error
}

func (s *stubInternalUserStore) Upsert(_ context.Context, email, provider, sub string) (User, error) {
	if s.err != nil {
		return User{}, s.err
	}
	if s.user.ID != "" {
		return s.user, nil
	}
	return User{ID: "stub-id", Email: email, Provider: provider, ProviderSub: sub}, nil
}

func (s *stubInternalUserStore) FindByID(_ context.Context, _ string) (User, error) {
	return s.user, s.err
}

// upsertDirect calls the internal store + event handler path that VerifyAndUpsert
// would reach after OIDC verification — lets us test event dispatch without a
// real OIDC token.
func upsertDirect(svc *oidcService, ctx context.Context, email, name string) (User, error) {
	user, err := svc.store.Upsert(ctx, email, "google", "sub-123")
	if err != nil {
		return User{}, err
	}
	if svc.eventHandler != nil {
		n := name
		if n == "" {
			n = email
		}
		if err := svc.eventHandler.OnUserVerified(ctx, user.ID, user.Email, n); err != nil {
			return User{}, err
		}
	}
	return user, nil
}

func TestOnUserVerified_CalledAfterSuccessfulUpsert(t *testing.T) {
	handler := &stubEventHandler{}
	svc := directService(&stubInternalUserStore{}, handler)

	user, err := upsertDirect(svc, context.Background(), "alice@example.com", "Alice")
	if err != nil {
		t.Fatalf("upsertDirect: %v", err)
	}

	if len(handler.calls) != 1 {
		t.Fatalf("expected 1 OnUserVerified call, got %d", len(handler.calls))
	}
	call := handler.calls[0]
	if call.userID != user.ID {
		t.Errorf("userID: got %q, want %q", call.userID, user.ID)
	}
	if call.email != "alice@example.com" {
		t.Errorf("email: got %q, want %q", call.email, "alice@example.com")
	}
	if call.name != "Alice" {
		t.Errorf("name: got %q, want %q", call.name, "Alice")
	}
}

func TestOnUserVerified_FallsBackToEmailWhenNameEmpty(t *testing.T) {
	handler := &stubEventHandler{}
	svc := directService(&stubInternalUserStore{}, handler)

	_, err := upsertDirect(svc, context.Background(), "bob@example.com", "")
	if err != nil {
		t.Fatalf("upsertDirect: %v", err)
	}

	if len(handler.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(handler.calls))
	}
	if handler.calls[0].name != "bob@example.com" {
		t.Errorf("name fallback: got %q, want %q", handler.calls[0].name, "bob@example.com")
	}
}

func TestOnUserVerified_NotCalledWhenUpsertFails(t *testing.T) {
	handler := &stubEventHandler{}
	svc := directService(&stubInternalUserStore{err: errors.New("db error")}, handler)

	_, err := upsertDirect(svc, context.Background(), "carol@example.com", "Carol")
	if err == nil {
		t.Fatal("expected error from store, got nil")
	}
	if len(handler.calls) != 0 {
		t.Errorf("expected 0 OnUserVerified calls on store error, got %d", len(handler.calls))
	}
}

func TestOnUserVerified_ErrorPropagates(t *testing.T) {
	handler := &stubEventHandler{err: errors.New("event handler error")}
	svc := directService(&stubInternalUserStore{}, handler)

	_, err := upsertDirect(svc, context.Background(), "dave@example.com", "Dave")
	if err == nil {
		t.Fatal("expected error from event handler, got nil")
	}
}

func TestOnUserVerified_NotCalledWhenHandlerNil(t *testing.T) {
	svc := directService(&stubInternalUserStore{}, nil)

	_, err := upsertDirect(svc, context.Background(), "eve@example.com", "Eve")
	if err != nil {
		t.Fatalf("expected no error with nil event handler, got %v", err)
	}
}

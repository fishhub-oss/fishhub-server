package provisioning_test

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type stubProvisioningStore struct {
	code   string
	getErr error

	claimedDeviceID string
	claimUserID     string
	claimErr        error

	activateErr error
}

func (s *stubProvisioningStore) GetOrCreateCode(_ context.Context, _ string) (string, error) {
	return s.code, s.getErr
}
func (s *stubProvisioningStore) ClaimCode(_ context.Context, _ string) (string, string, error) {
	uid := s.claimUserID
	if uid == "" {
		uid = "user-uuid"
	}
	return s.claimedDeviceID, uid, s.claimErr
}
func (s *stubProvisioningStore) Activate(_ context.Context, _ *sql.Tx, _, _, _ string) error {
	return s.activateErr
}

type stubOutboxStore struct {
	insertErr error
}

func (s *stubOutboxStore) ClaimBatch(_ context.Context, _ int) ([]outbox.Event, error) {
	return nil, nil
}
func (s *stubOutboxStore) MarkCompleted(_ context.Context, _ string) error { return nil }
func (s *stubOutboxStore) RecordFailure(_ context.Context, _ string, _, _ int, _ string) error {
	return nil
}
func (s *stubOutboxStore) Insert(_ context.Context, _ *sql.Tx, _ string, _ any, _ int) error {
	return s.insertErr
}

type stubSigner struct {
	token string
	err   error
}

func (s *stubSigner) Sign(_, _ string) (string, error) { return s.token, s.err }
func (s *stubSigner) PublicKey() *rsa.PublicKey         { return nil }
func (s *stubSigner) KID() string                       { return "" }
func (s *stubSigner) Issuer() string                    { return "" }

func newActivationSvc(t *testing.T, store provisioning.Store, outboxStore outbox.Store, signer *stubSigner) *provisioning.ActivationService {
	t.Helper()
	db := testutil.NewTestDB(t)
	return provisioning.NewActivationService(db, store, outboxStore, signer, discardLogger)
}

func TestActivationService_HappyPath(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	userID := platform.SeedUserID()

	provStore := provisioning.NewStore(db)
	outboxStore := outbox.NewPostgresStore(db)

	code, err := provStore.GetOrCreateCode(ctx, userID)
	if err != nil {
		t.Fatalf("setup: get code: %v", err)
	}

	svc := provisioning.NewActivationService(db, provStore, outboxStore, &stubSigner{token: "jwt-tok"}, discardLogger)

	result, err := svc.Activate(ctx, code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Token != "jwt-tok" {
		t.Errorf("token: got %q want %q", result.Token, "jwt-tok")
	}
	if result.DeviceID == "" {
		t.Error("expected non-empty device_id")
	}
}

func TestActivationService_CodeNotFound(t *testing.T) {
	svc := newActivationSvc(t,
		&stubProvisioningStore{claimErr: provisioning.ErrCodeNotFound},
		&stubOutboxStore{},
		&stubSigner{},
	)
	_, err := svc.Activate(context.Background(), "XXXXXX")
	if !errors.Is(err, provisioning.ErrCodeNotFound) {
		t.Errorf("expected ErrCodeNotFound, got %v", err)
	}
}

func TestActivationService_CodeAlreadyUsed(t *testing.T) {
	svc := newActivationSvc(t,
		&stubProvisioningStore{claimErr: provisioning.ErrCodeAlreadyUsed},
		&stubOutboxStore{},
		&stubSigner{},
	)
	_, err := svc.Activate(context.Background(), "XXXXXX")
	if !errors.Is(err, provisioning.ErrCodeAlreadyUsed) {
		t.Errorf("expected ErrCodeAlreadyUsed, got %v", err)
	}
}

func TestActivationService_ActivateStoreError(t *testing.T) {
	activateErr := errors.New("db error")
	svc := newActivationSvc(t,
		&stubProvisioningStore{claimedDeviceID: "dev-1", claimUserID: "usr-1", activateErr: activateErr},
		&stubOutboxStore{},
		&stubSigner{},
	)
	_, err := svc.Activate(context.Background(), "ABC123")
	if !errors.Is(err, activateErr) {
		t.Errorf("expected wrapped activateErr, got %v", err)
	}
}

func TestActivationService_SignerError(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	userID := platform.SeedUserID()

	provStore := provisioning.NewStore(db)
	outboxStore := outbox.NewPostgresStore(db)

	code, err := provStore.GetOrCreateCode(ctx, userID)
	if err != nil {
		t.Fatalf("setup: get code: %v", err)
	}

	signErr := errors.New("signing key not configured")
	svc := provisioning.NewActivationService(db, provStore, outboxStore, &stubSigner{err: signErr}, discardLogger)

	_, err = svc.Activate(ctx, code)
	if !errors.Is(err, signErr) {
		t.Errorf("expected wrapped signErr, got %v", err)
	}
}

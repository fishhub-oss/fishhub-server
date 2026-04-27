package sensors_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

func newActivationSvc(t *testing.T, store sensors.ProvisioningStore, outboxStore outbox.Store, signer *stubSigner) *sensors.ActivationService {
	t.Helper()
	db := testutil.NewTestDB(t)
	return sensors.NewActivationService(db, store, outboxStore, signer, discardLogger)
}

func TestActivationService_HappyPath(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	userID := platform.SeedUserID()

	provStore := sensors.NewProvisioningStore(db)
	outboxStore := outbox.NewPostgresStore(db)

	code, err := provStore.GetOrCreateCode(ctx, userID)
	if err != nil {
		t.Fatalf("setup: get code: %v", err)
	}

	svc := sensors.NewActivationService(db, provStore, outboxStore, &stubSigner{token: "jwt-tok"}, discardLogger)

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
		&stubProvisioningStore{claimErr: sensors.ErrCodeNotFound},
		&stubOutboxStore{},
		&stubSigner{},
	)
	_, err := svc.Activate(context.Background(), "XXXXXX")
	if !errors.Is(err, sensors.ErrCodeNotFound) {
		t.Errorf("expected ErrCodeNotFound, got %v", err)
	}
}

func TestActivationService_CodeAlreadyUsed(t *testing.T) {
	svc := newActivationSvc(t,
		&stubProvisioningStore{claimErr: sensors.ErrCodeAlreadyUsed},
		&stubOutboxStore{},
		&stubSigner{},
	)
	_, err := svc.Activate(context.Background(), "XXXXXX")
	if !errors.Is(err, sensors.ErrCodeAlreadyUsed) {
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

	provStore := sensors.NewProvisioningStore(db)
	outboxStore := outbox.NewPostgresStore(db)

	code, err := provStore.GetOrCreateCode(ctx, userID)
	if err != nil {
		t.Fatalf("setup: get code: %v", err)
	}

	signErr := errors.New("signing key not configured")
	svc := sensors.NewActivationService(db, provStore, outboxStore, &stubSigner{err: signErr}, discardLogger)

	_, err = svc.Activate(ctx, code)
	if !errors.Is(err, signErr) {
		t.Errorf("expected wrapped signErr, got %v", err)
	}
}

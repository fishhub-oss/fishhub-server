package provisioning_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
)

func TestProvisioningService_Provision_HappyPath(t *testing.T) {
	svc := provisioning.NewService(&stubProvisioningStore{code: "ABC123"}, discardLogger)
	code, err := svc.Provision(context.Background(), "usr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "ABC123" {
		t.Errorf("code: got %q want %q", code, "ABC123")
	}
}

func TestProvisioningService_Provision_StoreError(t *testing.T) {
	storeErr := errors.New("db down")
	svc := provisioning.NewService(&stubProvisioningStore{getErr: storeErr}, discardLogger)
	_, err := svc.Provision(context.Background(), "usr-1")
	if !errors.Is(err, storeErr) {
		t.Errorf("expected wrapped storeErr, got %v", err)
	}
}

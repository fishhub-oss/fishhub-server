package sensors_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

func TestProvisioningService_Provision_HappyPath(t *testing.T) {
	svc := &sensors.ProvisioningService{
		Store: &stubProvisioningStore{deviceID: "dev-uuid", code: "ABC123"},
	}
	deviceID, code, err := svc.Provision(context.Background(), "usr-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deviceID != "dev-uuid" {
		t.Errorf("device_id: got %q want %q", deviceID, "dev-uuid")
	}
	if code != "ABC123" {
		t.Errorf("code: got %q want %q", code, "ABC123")
	}
}

func TestProvisioningService_Provision_StoreError(t *testing.T) {
	storeErr := errors.New("db down")
	svc := &sensors.ProvisioningService{
		Store: &stubProvisioningStore{getErr: storeErr},
	}
	_, _, err := svc.Provision(context.Background(), "usr-1")
	if !errors.Is(err, storeErr) {
		t.Errorf("expected wrapped storeErr, got %v", err)
	}
}

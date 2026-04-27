package sensors_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

func newActivationSvc(store *stubProvisioningStore, mq *stubHiveMQClient, signer *stubSigner) *sensors.ActivationService {
	return sensors.NewActivationService(store, mq, signer, "broker.example.com", 8883, discardLogger)
}

func TestActivationService_HappyPath(t *testing.T) {
	svc := newActivationSvc(
		&stubProvisioningStore{claimedDeviceID: "dev-1", claimUserID: "usr-1"},
		&stubHiveMQClient{},
		&stubSigner{token: "jwt-tok"},
	)

	result, err := svc.Activate(context.Background(), "ABC123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Token != "jwt-tok" {
		t.Errorf("token: got %q want %q", result.Token, "jwt-tok")
	}
	if result.DeviceID != "dev-1" {
		t.Errorf("device_id: got %q want %q", result.DeviceID, "dev-1")
	}
	if result.MQTTHost != "broker.example.com" {
		t.Errorf("mqtt_host: got %q want %q", result.MQTTHost, "broker.example.com")
	}
	if result.MQTTPort != 8883 {
		t.Errorf("mqtt_port: got %d want %d", result.MQTTPort, 8883)
	}
}

func TestActivationService_CodeNotFound(t *testing.T) {
	svc := newActivationSvc(
		&stubProvisioningStore{claimErr: sensors.ErrCodeNotFound},
		&stubHiveMQClient{},
		&stubSigner{},
	)
	_, err := svc.Activate(context.Background(), "XXXXXX")
	if !errors.Is(err, sensors.ErrCodeNotFound) {
		t.Errorf("expected ErrCodeNotFound, got %v", err)
	}
}

func TestActivationService_CodeAlreadyUsed(t *testing.T) {
	svc := newActivationSvc(
		&stubProvisioningStore{claimErr: sensors.ErrCodeAlreadyUsed},
		&stubHiveMQClient{},
		&stubSigner{},
	)
	_, err := svc.Activate(context.Background(), "XXXXXX")
	if !errors.Is(err, sensors.ErrCodeAlreadyUsed) {
		t.Errorf("expected ErrCodeAlreadyUsed, got %v", err)
	}
}

func TestActivationService_HiveMQError(t *testing.T) {
	provisionErr := errors.New("hivemq unavailable")
	svc := newActivationSvc(
		&stubProvisioningStore{claimedDeviceID: "dev-1", claimUserID: "usr-1"},
		&stubHiveMQClient{err: provisionErr},
		&stubSigner{},
	)
	_, err := svc.Activate(context.Background(), "ABC123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, provisionErr) {
		t.Errorf("expected wrapped provisionErr, got %v", err)
	}
}

func TestActivationService_ActivateStoreError(t *testing.T) {
	activateErr := errors.New("db error")
	svc := newActivationSvc(
		&stubProvisioningStore{claimedDeviceID: "dev-1", claimUserID: "usr-1", activateErr: activateErr},
		&stubHiveMQClient{},
		&stubSigner{},
	)
	_, err := svc.Activate(context.Background(), "ABC123")
	if !errors.Is(err, activateErr) {
		t.Errorf("expected wrapped activateErr, got %v", err)
	}
}

func TestActivationService_SignerError(t *testing.T) {
	signErr := errors.New("signing key not configured")
	svc := newActivationSvc(
		&stubProvisioningStore{claimedDeviceID: "dev-1", claimUserID: "usr-1"},
		&stubHiveMQClient{},
		&stubSigner{err: signErr},
	)
	_, err := svc.Activate(context.Background(), "ABC123")
	if !errors.Is(err, signErr) {
		t.Errorf("expected wrapped signErr, got %v", err)
	}
}

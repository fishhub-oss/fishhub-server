package provisioning_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
)

type stubProvisioner struct{ err error }

func (s *stubProvisioner) ProvisionDevice(_ context.Context, _, _ string) error { return s.err }
func (s *stubProvisioner) DeleteDevice(_ context.Context, _ string) error        { return s.err }

var errSentinel = errNew("store error")

func errNew(msg string) error {
	return &sentinelError{msg: msg}
}

type sentinelError struct{ msg string }

func (e *sentinelError) Error() string { return e.msg }

func TestMQTTProvisionProcessor_HappyPath(t *testing.T) {
	proc := provisioning.NewMQTTProvisionProcessor(&stubProvisioner{}, discardLogger)

	payload, _ := json.Marshal(provisioning.MQTTProvisionPayload{
		DeviceID: "dev-1",
		Username: "dev-1",
		Password: "secret",
	})

	if err := proc.Process(context.Background(), outbox.Event{Payload: payload}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMQTTProvisionProcessor_AlreadyExists(t *testing.T) {
	proc := provisioning.NewMQTTProvisionProcessor(&stubProvisioner{err: &sentinelError{msg: "409 conflict"}}, discardLogger)

	payload, _ := json.Marshal(provisioning.MQTTProvisionPayload{
		DeviceID: "dev-1",
		Username: "dev-1",
		Password: "secret",
	})

	if err := proc.Process(context.Background(), outbox.Event{Payload: payload}); err != nil {
		t.Fatalf("409 should be treated as success, got: %v", err)
	}
}

func TestMQTTProvisionProcessor_EventType(t *testing.T) {
	proc := provisioning.NewMQTTProvisionProcessor(&stubProvisioner{}, discardLogger)
	if proc.EventType() != provisioning.EventTypeMQTTProvision {
		t.Errorf("unexpected event type: %q", proc.EventType())
	}
}

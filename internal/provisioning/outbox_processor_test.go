package provisioning_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
)

type stubHiveMQClient struct{ err error }

func (s *stubHiveMQClient) ProvisionDevice(_ context.Context, _, _ string) error { return s.err }
func (s *stubHiveMQClient) DeleteDevice(_ context.Context, _ string) error        { return s.err }

var errSentinel = errNew("store error")

func errNew(msg string) error {
	return &sentinelError{msg: msg}
}

type sentinelError struct{ msg string }

func (e *sentinelError) Error() string { return e.msg }

func TestHiveMQProvisionProcessor_HappyPath(t *testing.T) {
	client := &stubHiveMQClient{}
	proc := provisioning.NewHiveMQProvisionProcessor(client, discardLogger)

	payload, _ := json.Marshal(provisioning.HiveMQProvisionPayload{
		DeviceID: "dev-1",
		Username: "dev-1",
		Password: "secret",
	})

	if err := proc.Process(context.Background(), outbox.Event{Payload: payload}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHiveMQProvisionProcessor_AlreadyExists(t *testing.T) {
	client := &stubHiveMQClient{err: &sentinelError{msg: "409 conflict"}}
	proc := provisioning.NewHiveMQProvisionProcessor(client, discardLogger)

	payload, _ := json.Marshal(provisioning.HiveMQProvisionPayload{
		DeviceID: "dev-1",
		Username: "dev-1",
		Password: "secret",
	})

	if err := proc.Process(context.Background(), outbox.Event{Payload: payload}); err != nil {
		t.Fatalf("409 should be treated as success, got: %v", err)
	}
}

func TestHiveMQProvisionProcessor_EventType(t *testing.T) {
	proc := provisioning.NewHiveMQProvisionProcessor(&stubHiveMQClient{}, discardLogger)
	if proc.EventType() != provisioning.EventTypeHiveMQProvision {
		t.Errorf("unexpected event type: %q", proc.EventType())
	}
}

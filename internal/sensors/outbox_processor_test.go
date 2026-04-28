package sensors_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

func TestPeripheralPushProcessor_create(t *testing.T) {
	pub := &stubPublisher{}
	proc := sensors.NewPeripheralPushProcessor(pub, discardLogger)

	payload, _ := json.Marshal(sensors.PeripheralPushPayload{
		DeviceID: "dev-1",
		Name:     "light",
		Op:       "create",
		Kind:     "relay",
		Pin:      5,
	})

	if err := proc.Process(context.Background(), outbox.Event{Payload: payload}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pub.called {
		t.Fatal("expected publisher to be called")
	}
	wantTopic := "fishhub/dev-1/peripherals/light"
	if pub.publishedTopic != wantTopic {
		t.Errorf("expected topic %q, got %q", wantTopic, pub.publishedTopic)
	}
	var msg sensors.PeripheralPushPayload
	if err := json.Unmarshal(pub.publishedPayload, &msg); err != nil {
		t.Fatalf("unmarshal published payload: %v", err)
	}
	if msg.Op != "create" || msg.Kind != "relay" || msg.Pin != 5 {
		t.Errorf("unexpected published payload: %+v", msg)
	}
}

func TestPeripheralPushProcessor_delete(t *testing.T) {
	pub := &stubPublisher{}
	proc := sensors.NewPeripheralPushProcessor(pub, discardLogger)

	payload, _ := json.Marshal(sensors.PeripheralPushPayload{
		DeviceID: "dev-1",
		Name:     "light",
		Op:       "delete",
	})

	if err := proc.Process(context.Background(), outbox.Event{Payload: payload}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var msg sensors.PeripheralPushPayload
	if err := json.Unmarshal(pub.publishedPayload, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Op != "delete" {
		t.Errorf("expected op 'delete', got %q", msg.Op)
	}
}

func TestPeripheralPushProcessor_publishError(t *testing.T) {
	pub := &stubPublisher{err: errSentinel}
	proc := sensors.NewPeripheralPushProcessor(pub, discardLogger)

	payload, _ := json.Marshal(sensors.PeripheralPushPayload{DeviceID: "d", Name: "n", Op: "create"})
	err := proc.Process(context.Background(), outbox.Event{Payload: payload})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPeripheralPushProcessor_eventType(t *testing.T) {
	proc := sensors.NewPeripheralPushProcessor(&stubPublisher{}, discardLogger)
	if proc.EventType() != sensors.EventTypePeripheralPush {
		t.Errorf("unexpected event type: %q", proc.EventType())
	}
}

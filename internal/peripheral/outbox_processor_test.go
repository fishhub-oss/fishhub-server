package peripheral_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

var errSentinel = errors.New("store error")

type stubPublisher struct {
	publishedTopic   string
	publishedPayload []byte
	called           bool
	err              error
}

func (s *stubPublisher) Publish(_ context.Context, topic string, payload []byte) error {
	s.publishedTopic = topic
	s.publishedPayload = payload
	s.called = true
	return s.err
}

func (s *stubPublisher) PublishRetained(_ context.Context, topic string, payload []byte) error {
	s.publishedTopic = topic
	s.publishedPayload = payload
	s.called = true
	return s.err
}

// peripheralPushPayload mirrors the unexported type for test assertions.
type peripheralPushPayload struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Op       string `json:"op"`
	Kind     string `json:"kind,omitempty"`
	Pin      int    `json:"pin,omitempty"`
}

func TestPeripheralPushProcessor_create(t *testing.T) {
	pub := &stubPublisher{}
	proc := peripheral.NewPeripheralPushProcessor(pub, discardLogger)

	payload, _ := json.Marshal(peripheralPushPayload{
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
	var msg peripheralPushPayload
	if err := json.Unmarshal(pub.publishedPayload, &msg); err != nil {
		t.Fatalf("unmarshal published payload: %v", err)
	}
	if msg.Op != "create" || msg.Kind != "relay" || msg.Pin != 5 {
		t.Errorf("unexpected published payload: %+v", msg)
	}
}

func TestPeripheralPushProcessor_delete(t *testing.T) {
	pub := &stubPublisher{}
	proc := peripheral.NewPeripheralPushProcessor(pub, discardLogger)

	payload, _ := json.Marshal(peripheralPushPayload{
		DeviceID: "dev-1",
		Name:     "light",
		Op:       "delete",
	})

	if err := proc.Process(context.Background(), outbox.Event{Payload: payload}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var msg peripheralPushPayload
	if err := json.Unmarshal(pub.publishedPayload, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Op != "delete" {
		t.Errorf("expected op 'delete', got %q", msg.Op)
	}
}

func TestPeripheralPushProcessor_publishError(t *testing.T) {
	pub := &stubPublisher{err: errSentinel}
	proc := peripheral.NewPeripheralPushProcessor(pub, discardLogger)

	payload, _ := json.Marshal(peripheralPushPayload{DeviceID: "d", Name: "n", Op: "create"})
	err := proc.Process(context.Background(), outbox.Event{Payload: payload})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestPeripheralPushProcessor_eventType(t *testing.T) {
	proc := peripheral.NewPeripheralPushProcessor(&stubPublisher{}, discardLogger)
	if proc.EventType() != "peripheral.push" {
		t.Errorf("unexpected event type: %q", proc.EventType())
	}
}

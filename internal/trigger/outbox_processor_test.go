package trigger_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type stubPublisher struct {
	calls    []publishCall
	retainCalled bool
	err      error
}

type publishCall struct {
	topic   string
	payload []byte
	retain  bool
}

func (s *stubPublisher) Publish(_ context.Context, topic string, payload []byte) error {
	s.calls = append(s.calls, publishCall{topic: topic, payload: payload, retain: false})
	return s.err
}

func (s *stubPublisher) PublishRetained(_ context.Context, topic string, payload []byte) error {
	s.calls = append(s.calls, publishCall{topic: topic, payload: payload, retain: true})
	s.retainCalled = true
	return s.err
}

func makeEvent(t *testing.T, payload any) outbox.Event {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal event payload: %v", err)
	}
	return outbox.Event{Payload: b}
}

type pushPayload struct {
	Op               string          `json:"op"`
	ID               string          `json:"id"`
	DeviceID         string          `json:"device_id"`
	Enabled          bool            `json:"enabled"`
	Condition        json.RawMessage `json:"condition"`
	TargetPeripheral string          `json:"target_peripheral"`
	Action           json.RawMessage `json:"action"`
	CooldownS        int             `json:"cooldown_s"`
}

func TestTriggerPushProcessor_EventType(t *testing.T) {
	proc := trigger.NewTriggerPushProcessor(&stubPublisher{}, discardLogger)
	if proc.EventType() != "trigger.push" {
		t.Errorf("unexpected event type: %q", proc.EventType())
	}
}

func TestTriggerPushProcessor_Upsert(t *testing.T) {
	pub := &stubPublisher{}
	proc := trigger.NewTriggerPushProcessor(pub, discardLogger)

	payload := pushPayload{
		Op:               "upsert",
		ID:               "trig-1",
		DeviceID:         "dev-1",
		Enabled:          true,
		Condition:        json.RawMessage(`{"op":"lt","left":{"op":"value","measurement":"ds18b20-4/temperature"},"right":{"op":"literal","value":19.0}}`),
		TargetPeripheral: "relay-14",
		Action:           json.RawMessage(`{"action":"set","value":1.0}`),
		CooldownS:        60,
	}

	if err := proc.Process(context.Background(), makeEvent(t, payload)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(pub.calls))
	}
	call := pub.calls[0]
	if !call.retain {
		t.Error("expected retained publish")
	}
	wantTopic := "fishhub/dev-1/triggers/trig-1"
	if call.topic != wantTopic {
		t.Errorf("topic: got %q, want %q", call.topic, wantTopic)
	}

	var msg pushPayload
	if err := json.Unmarshal(call.payload, &msg); err != nil {
		t.Fatalf("unmarshal published payload: %v", err)
	}
	if msg.Op != "upsert" {
		t.Errorf("op: got %q, want %q", msg.Op, "upsert")
	}
	if msg.ID != "trig-1" {
		t.Errorf("id: got %q, want %q", msg.ID, "trig-1")
	}
	if msg.TargetPeripheral != "relay-14" {
		t.Errorf("target_peripheral: got %q, want %q", msg.TargetPeripheral, "relay-14")
	}
	if msg.CooldownS != 60 {
		t.Errorf("cooldown_s: got %d, want 60", msg.CooldownS)
	}
	// device_id must NOT appear in the MQTT message
	if msg.DeviceID != "" {
		t.Errorf("device_id should not be in MQTT payload, got %q", msg.DeviceID)
	}
}

func TestTriggerPushProcessor_Delete(t *testing.T) {
	pub := &stubPublisher{}
	proc := trigger.NewTriggerPushProcessor(pub, discardLogger)

	payload := pushPayload{
		Op:       "delete",
		ID:       "trig-2",
		DeviceID: "dev-1",
	}

	if err := proc.Process(context.Background(), makeEvent(t, payload)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pub.calls) != 2 {
		t.Fatalf("expected 2 publish calls (delete + clear), got %d", len(pub.calls))
	}

	// First call: delete payload
	deleteCall := pub.calls[0]
	wantTopic := "fishhub/dev-1/triggers/trig-2"
	if deleteCall.topic != wantTopic {
		t.Errorf("delete topic: got %q, want %q", deleteCall.topic, wantTopic)
	}
	var msg struct {
		Op string `json:"op"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(deleteCall.payload, &msg); err != nil {
		t.Fatalf("unmarshal delete payload: %v", err)
	}
	if msg.Op != "delete" || msg.ID != "trig-2" {
		t.Errorf("unexpected delete message: op=%q id=%q", msg.Op, msg.ID)
	}

	// Second call: empty payload to clear retained message
	clearCall := pub.calls[1]
	if clearCall.topic != wantTopic {
		t.Errorf("clear topic: got %q, want %q", clearCall.topic, wantTopic)
	}
	if len(clearCall.payload) != 0 {
		t.Errorf("expected empty clear payload, got %q", clearCall.payload)
	}
}

func TestTriggerPushProcessor_PublishError(t *testing.T) {
	pub := &stubPublisher{err: errors.New("broker down")}
	proc := trigger.NewTriggerPushProcessor(pub, discardLogger)

	payload := pushPayload{Op: "upsert", ID: "t1", DeviceID: "d1",
		Condition: json.RawMessage(`{}`), Action: json.RawMessage(`{}`)}

	if err := proc.Process(context.Background(), makeEvent(t, payload)); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTriggerPushProcessor_DeletePublishError(t *testing.T) {
	pub := &stubPublisher{err: errors.New("broker down")}
	proc := trigger.NewTriggerPushProcessor(pub, discardLogger)

	payload := pushPayload{Op: "delete", ID: "t1", DeviceID: "d1"}

	if err := proc.Process(context.Background(), makeEvent(t, payload)); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTriggerPushProcessor_InvalidPayload(t *testing.T) {
	pub := &stubPublisher{}
	proc := trigger.NewTriggerPushProcessor(pub, discardLogger)

	if err := proc.Process(context.Background(), outbox.Event{Payload: []byte("not json")}); err == nil {
		t.Fatal("expected error for invalid payload, got nil")
	}
}

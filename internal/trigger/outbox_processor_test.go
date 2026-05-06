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
	calls        []publishCall
	retainCalled bool
	err          error
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

// internalPayload mirrors the internal triggerPushPayload struct for test construction.
type internalPayload struct {
	Op        string          `json:"op"`
	ID        string          `json:"id"`
	DeviceID  string          `json:"device_id"`
	Enabled   bool            `json:"enabled,omitempty"`
	Condition json.RawMessage `json:"condition,omitempty"`
	Actions   []actionPayload `json:"actions,omitempty"`
	CooldownS int             `json:"cooldown_s,omitempty"`
}

type actionPayload struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

// mqttUpsertMessage mirrors the MQTT wire format published for upsert events.
type mqttUpsertMessage struct {
	Op        string          `json:"op"`
	ID        string          `json:"id"`
	DeviceID  string          `json:"device_id"`
	Enabled   bool            `json:"enabled"`
	Condition json.RawMessage `json:"condition"`
	Actions   []actionPayload `json:"actions"`
	CooldownS int             `json:"cooldown_s"`
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

	actionConfig, _ := json.Marshal(map[string]any{
		"peripheral_id": "p-uuid",
		"peripheral":    "relay-14",
		"command":       "set",
		"value":         1.0,
	})

	payload := internalPayload{
		Op:        "upsert",
		ID:        "trig-1",
		DeviceID:  "dev-1",
		Enabled:   true,
		Condition: json.RawMessage(`{"op":"lt","left":{"op":"value","measurement":"ds18b20-4/temperature"},"right":{"op":"literal","value":19.0}}`),
		Actions:   []actionPayload{{Type: "peripheral_action", Config: actionConfig}},
		CooldownS: 60,
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

	var msg mqttUpsertMessage
	if err := json.Unmarshal(call.payload, &msg); err != nil {
		t.Fatalf("unmarshal published payload: %v", err)
	}
	if msg.Op != "upsert" {
		t.Errorf("op: got %q, want upsert", msg.Op)
	}
	if msg.ID != "trig-1" {
		t.Errorf("id: got %q, want trig-1", msg.ID)
	}
	if msg.CooldownS != 60 {
		t.Errorf("cooldown_s: got %d, want 60", msg.CooldownS)
	}
	// device_id must NOT appear in the MQTT message.
	if msg.DeviceID != "" {
		t.Errorf("device_id should not be in MQTT payload, got %q", msg.DeviceID)
	}
	if len(msg.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(msg.Actions))
	}
	if msg.Actions[0].Type != "peripheral_action" {
		t.Errorf("action type: got %q, want peripheral_action", msg.Actions[0].Type)
	}

	// Config must carry the peripheral kind-pin and command.
	var cfg struct {
		Peripheral string `json:"peripheral"`
		Command    string `json:"command"`
	}
	if err := json.Unmarshal(msg.Actions[0].Config, &cfg); err != nil {
		t.Fatalf("unmarshal action config: %v", err)
	}
	if cfg.Peripheral != "relay-14" {
		t.Errorf("peripheral: got %q, want relay-14", cfg.Peripheral)
	}
	if cfg.Command != "set" {
		t.Errorf("command: got %q, want set", cfg.Command)
	}

	// Old flat fields must be absent.
	var raw map[string]json.RawMessage
	json.Unmarshal(call.payload, &raw)
	if _, found := raw["target_peripheral"]; found {
		t.Error("target_peripheral should not appear in MQTT message")
	}
	if _, found := raw["action"]; found {
		t.Error("action should not appear in MQTT message")
	}
}

func TestTriggerPushProcessor_Upsert_MultipleActions(t *testing.T) {
	pub := &stubPublisher{}
	proc := trigger.NewTriggerPushProcessor(pub, discardLogger)

	cfg1, _ := json.Marshal(map[string]any{"peripheral": "relay-14", "command": "set", "value": 1.0})
	cfg2, _ := json.Marshal(map[string]any{"peripheral": "relay-15", "command": "set", "value": 0.0})

	payload := internalPayload{
		Op:        "upsert",
		ID:        "trig-2",
		DeviceID:  "dev-1",
		Enabled:   true,
		Condition: json.RawMessage(`{}`),
		Actions: []actionPayload{
			{Type: "peripheral_action", Config: cfg1},
			{Type: "peripheral_action", Config: cfg2},
		},
		CooldownS: 30,
	}

	if err := proc.Process(context.Background(), makeEvent(t, payload)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var msg mqttUpsertMessage
	json.Unmarshal(pub.calls[0].payload, &msg)
	if len(msg.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(msg.Actions))
	}
}

func TestTriggerPushProcessor_Delete(t *testing.T) {
	pub := &stubPublisher{}
	proc := trigger.NewTriggerPushProcessor(pub, discardLogger)

	payload := internalPayload{
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

	actionConfig, _ := json.Marshal(map[string]any{"peripheral": "relay-14", "command": "set"})
	payload := internalPayload{
		Op:        "upsert",
		ID:        "t1",
		DeviceID:  "d1",
		Condition: json.RawMessage(`{}`),
		Actions:   []actionPayload{{Type: "peripheral_action", Config: actionConfig}},
	}

	if err := proc.Process(context.Background(), makeEvent(t, payload)); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTriggerPushProcessor_DeletePublishError(t *testing.T) {
	pub := &stubPublisher{err: errors.New("broker down")}
	proc := trigger.NewTriggerPushProcessor(pub, discardLogger)

	payload := internalPayload{Op: "delete", ID: "t1", DeviceID: "d1"}

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

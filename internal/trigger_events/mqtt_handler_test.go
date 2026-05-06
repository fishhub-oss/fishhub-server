package trigger_events_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	trigger_events "github.com/fishhub-oss/fishhub-server/internal/trigger_events"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// ── stubStore ─────────────────────────────────────────────────────────────────

type stubStore struct {
	ingestCalled bool
	ingestEvent  trigger_events.TriggerEvent
	ingestErr    error

	belongsResult bool
	belongsErr    error
}

func (s *stubStore) Ingest(_ context.Context, e trigger_events.TriggerEvent) error {
	s.ingestCalled = true
	s.ingestEvent = e
	return s.ingestErr
}

func (s *stubStore) ListByTrigger(_ context.Context, _ string, _ int) ([]trigger_events.TriggerEvent, error) {
	return nil, nil
}

func (s *stubStore) TriggerBelongsToDevice(_ context.Context, _, _ string) (bool, error) {
	return s.belongsResult, s.belongsErr
}

// ── tests ─────────────────────────────────────────────────────────────────────

const validPayload = `{
	"trigger_event_id": "abc123",
	"trigger_id":       "11111111-1111-1111-1111-111111111111",
	"device_id":        "22222222-2222-2222-2222-222222222222",
	"fired_at":         "2025-05-05T14:00:00Z",
	"readings": [{"peripheral": "ds18b20-4/temperature", "value": 18.5}]
}`

func newHandler(store trigger_events.Store) *trigger_events.MQTTHandler {
	return trigger_events.NewMQTTHandler(store, discardLogger)
}

func TestMQTTHandler_Handle(t *testing.T) {
	t.Run("valid message ingests event", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		h := newHandler(store)

		h.Handle(context.Background(), "fishhub/22222222-2222-2222-2222-222222222222/trigger_events", []byte(validPayload))

		if !store.ingestCalled {
			t.Fatal("expected Ingest to be called")
		}
		if store.ingestEvent.TriggerEventID != "abc123" {
			t.Errorf("trigger_event_id: got %q", store.ingestEvent.TriggerEventID)
		}
		if len(store.ingestEvent.Readings) != 1 {
			t.Errorf("readings: expected 1, got %d", len(store.ingestEvent.Readings))
		}
	})

	t.Run("trigger not belonging to device drops message", func(t *testing.T) {
		store := &stubStore{belongsResult: false}
		h := newHandler(store)

		h.Handle(context.Background(), "fishhub/device-x/trigger_events", []byte(validPayload))

		if store.ingestCalled {
			t.Error("expected Ingest not to be called")
		}
	})

	t.Run("malformed topic drops message", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		h := newHandler(store)

		for _, topic := range []string{
			"fishhub/trigger_events",
			"fishhub/dev/trigger_events/extra",
			"other/dev/trigger_events",
			"fishhub//trigger_events",
		} {
			store.ingestCalled = false
			h.Handle(context.Background(), topic, []byte(validPayload))
			if store.ingestCalled {
				t.Errorf("topic %q: expected Ingest not to be called", topic)
			}
		}
	})

	t.Run("malformed JSON drops message", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		h := newHandler(store)

		h.Handle(context.Background(), "fishhub/dev/trigger_events", []byte("not json"))

		if store.ingestCalled {
			t.Error("expected Ingest not to be called on bad payload")
		}
	})

	t.Run("missing required fields drops message", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		h := newHandler(store)

		h.Handle(context.Background(), "fishhub/dev/trigger_events", []byte(`{"trigger_id":"x"}`))

		if store.ingestCalled {
			t.Error("expected Ingest not to be called on missing fields")
		}
	})

	t.Run("invalid fired_at drops message", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		h := newHandler(store)

		payload := `{"trigger_event_id":"x","trigger_id":"t1","device_id":"d1","fired_at":"not-a-date","readings":[]}`
		h.Handle(context.Background(), "fishhub/dev/trigger_events", []byte(payload))

		if store.ingestCalled {
			t.Error("expected Ingest not to be called on invalid fired_at")
		}
	})
}

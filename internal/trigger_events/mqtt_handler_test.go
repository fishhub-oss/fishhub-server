package trigger_events_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/queue"
	trigger_events "github.com/fishhub-oss/fishhub-server/internal/trigger_events"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// ── stubStore ─────────────────────────────────────────────────────────────────

type stubStore struct {
	ingestCalled bool
	ingestEvent  trigger_events.TriggerEvent
	ingestErr    error
	ingestResult trigger_events.TriggerEvent

	belongsResult bool
	belongsErr    error
}

func (s *stubStore) Ingest(_ context.Context, e trigger_events.TriggerEvent) (trigger_events.TriggerEvent, error) {
	s.ingestCalled = true
	s.ingestEvent = e
	if s.ingestResult.ID == "" {
		s.ingestResult = e
		s.ingestResult.ID = "stub-event-id"
	}
	return s.ingestResult, s.ingestErr
}

func (s *stubStore) ListByTriggerCursor(_ context.Context, _ string, _ trigger_events.CursorPage) ([]trigger_events.TriggerEvent, error) {
	return nil, nil
}

func (s *stubStore) TriggerBelongsToDevice(_ context.Context, _, _ string) (bool, error) {
	return s.belongsResult, s.belongsErr
}

// ── stubActionGetter ──────────────────────────────────────────────────────────

type stubActionGetter struct {
	actions []trigger_events.TriggerAction
	err     error
}

func (s *stubActionGetter) GetActions(_ context.Context, _ string) ([]trigger_events.TriggerAction, error) {
	return s.actions, s.err
}

// ── spyQueue ──────────────────────────────────────────────────────────────────

type enqueuedJob struct {
	queue string
	job   queue.Job
}

type spyQueue struct {
	jobs []enqueuedJob
}

func (q *spyQueue) Enqueue(_ context.Context, queueName string, job queue.Job) error {
	q.jobs = append(q.jobs, enqueuedJob{queue: queueName, job: job})
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newHandler(store trigger_events.Store, actions trigger_events.ActionGetter, q queue.Queue) *trigger_events.MQTTHandler {
	return trigger_events.NewMQTTHandler(store, actions, q, discardLogger)
}

func noopHandler(store trigger_events.Store) *trigger_events.MQTTHandler {
	return newHandler(store, &stubActionGetter{}, queue.NewNoOpQueue(discardLogger))
}

const validPayload = `{
	"trigger_event_id": "abc123",
	"trigger_id":       "11111111-1111-1111-1111-111111111111",
	"device_id":        "22222222-2222-2222-2222-222222222222",
	"fired_at":         "2025-05-05T14:00:00Z",
	"readings": [{"peripheral": "ds18b20-4/temperature", "value": 18.5}]
}`

// ── tests ─────────────────────────────────────────────────────────────────────

func TestMQTTHandler_Handle(t *testing.T) {
	t.Run("valid message ingests event", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		h := noopHandler(store)

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
		h := noopHandler(store)

		h.Handle(context.Background(), "fishhub/device-x/trigger_events", []byte(validPayload))

		if store.ingestCalled {
			t.Error("expected Ingest not to be called")
		}
	})

	t.Run("malformed topic drops message", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		h := noopHandler(store)

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
		h := noopHandler(store)

		h.Handle(context.Background(), "fishhub/dev/trigger_events", []byte("not json"))

		if store.ingestCalled {
			t.Error("expected Ingest not to be called on bad payload")
		}
	})

	t.Run("missing required fields drops message", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		h := noopHandler(store)

		h.Handle(context.Background(), "fishhub/dev/trigger_events", []byte(`{"trigger_id":"x"}`))

		if store.ingestCalled {
			t.Error("expected Ingest not to be called on missing fields")
		}
	})

	t.Run("invalid fired_at drops message", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		h := noopHandler(store)

		payload := `{"trigger_event_id":"x","trigger_id":"t1","device_id":"d1","fired_at":"not-a-date","readings":[]}`
		h.Handle(context.Background(), "fishhub/dev/trigger_events", []byte(payload))

		if store.ingestCalled {
			t.Error("expected Ingest not to be called on invalid fired_at")
		}
	})

	t.Run("server-side action is enqueued after ingest", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		actions := &stubActionGetter{
			actions: []trigger_events.TriggerAction{
				{ID: "action-1", Type: "alert", Config: json.RawMessage(`{"severity":"warning"}`)},
			},
		}
		spy := &spyQueue{}
		h := newHandler(store, actions, spy)

		h.Handle(context.Background(), "fishhub/22222222-2222-2222-2222-222222222222/trigger_events", []byte(validPayload))

		if len(spy.jobs) != 1 {
			t.Fatalf("expected 1 enqueued job, got %d", len(spy.jobs))
		}
		if spy.jobs[0].job.Type != "alert" {
			t.Errorf("job type: got %q, want %q", spy.jobs[0].job.Type, "alert")
		}
		if spy.jobs[0].queue != "trigger-actions" {
			t.Errorf("queue name: got %q, want %q", spy.jobs[0].queue, "trigger-actions")
		}

		var p queue.AlertJobPayload
		if err := json.Unmarshal(spy.jobs[0].job.Payload, &p); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if p.ActionID != "action-1" {
			t.Errorf("action_id: got %q, want %q", p.ActionID, "action-1")
		}
		if p.TriggerID != "11111111-1111-1111-1111-111111111111" {
			t.Errorf("trigger_id: got %q", p.TriggerID)
		}
		if len(p.Readings) != 1 {
			t.Errorf("readings: expected 1, got %d", len(p.Readings))
		}
	})

	t.Run("peripheral_action is not enqueued", func(t *testing.T) {
		store := &stubStore{belongsResult: true}
		actions := &stubActionGetter{
			actions: []trigger_events.TriggerAction{
				{ID: "action-2", Type: "peripheral_action", Config: json.RawMessage(`{}`)},
			},
		}
		spy := &spyQueue{}
		h := newHandler(store, actions, spy)

		h.Handle(context.Background(), "fishhub/22222222-2222-2222-2222-222222222222/trigger_events", []byte(validPayload))

		if len(spy.jobs) != 0 {
			t.Errorf("expected 0 enqueued jobs for peripheral_action, got %d", len(spy.jobs))
		}
	})
}

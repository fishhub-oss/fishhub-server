package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/api"
	trigger_events "github.com/fishhub-oss/fishhub-server/internal/trigger_events"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
)

// ── stubTriggerEventStore ─────────────────────────────────────────────────────

type stubTriggerEventStore struct {
	events  []trigger_events.TriggerEvent
	listErr error
}

func (s *stubTriggerEventStore) Ingest(_ context.Context, e trigger_events.TriggerEvent) (trigger_events.TriggerEvent, error) {
	return e, nil
}
func (s *stubTriggerEventStore) ListByTriggerCursor(_ context.Context, _ string, _ trigger_events.CursorPage) ([]trigger_events.TriggerEvent, error) {
	return s.events, s.listErr
}
func (s *stubTriggerEventStore) TriggerBelongsToDevice(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newListTriggerEventsHandler(triggerStore trigger.Store, eventStore trigger_events.Store) *api.ListTriggerEventsHandler {
	return &api.ListTriggerEventsHandler{TriggerStore: triggerStore, EventStore: eventStore}
}

func makeFullPage() []trigger_events.TriggerEvent {
	base := time.Date(2025, 5, 5, 14, 0, 0, 0, time.UTC)
	events := make([]trigger_events.TriggerEvent, 20) // matches defaultEventPageSize
	for i := range 20 {
		events[i] = trigger_events.TriggerEvent{
			ID:         fmt.Sprintf("id-%d", i),
			TriggerID:  "trig-1",
			FiredAt:    base.Add(-time.Duration(i) * time.Minute),
			ReceivedAt: base.Add(-time.Duration(i)*time.Minute + time.Second),
			Readings:   []trigger_events.Reading{},
		}
	}
	return events
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestListTriggerEventsHandler(t *testing.T) {
	firedAt := time.Date(2025, 5, 5, 14, 0, 0, 0, time.UTC)
	receivedAt := firedAt.Add(time.Second)

	sampleEvents := []trigger_events.TriggerEvent{
		{
			ID:             "evt-uuid-1",
			TriggerEventID: "abc123",
			TriggerID:      "trig-1",
			FiredAt:        firedAt,
			ReceivedAt:     receivedAt,
			Readings: []trigger_events.Reading{
				{Peripheral: "ds18b20-4/temperature", Value: 18.5},
			},
		},
	}

	t.Run("returns 200 with events and readings", func(t *testing.T) {
		triggerStore := &stubTriggerStore{got: newTrigger()}
		eventStore := &stubTriggerEventStore{events: sampleEvents}
		h := newListTriggerEventsHandler(triggerStore, eventStore)

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		events, ok := resp["events"].([]any)
		if !ok || len(events) != 1 {
			t.Fatalf("expected 1 event, got %v", resp["events"])
		}
		evt := events[0].(map[string]any)
		if evt["trigger_event_id"] != "abc123" {
			t.Errorf("trigger_event_id: got %v", evt["trigger_event_id"])
		}
		readings, ok := evt["readings"].([]any)
		if !ok || len(readings) != 1 {
			t.Errorf("expected 1 reading, got %v", evt["readings"])
		}
		r := readings[0].(map[string]any)
		if r["peripheral"] != "ds18b20-4/temperature" {
			t.Errorf("peripheral: got %v", r["peripheral"])
		}
	})

	t.Run("no next_cursor when fewer than page size events returned", func(t *testing.T) {
		triggerStore := &stubTriggerStore{got: newTrigger()}
		eventStore := &stubTriggerEventStore{events: sampleEvents} // 1 < 20
		h := newListTriggerEventsHandler(triggerStore, eventStore)

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
		if _, hasCursor := resp["next_cursor"]; hasCursor {
			t.Error("expected no next_cursor when results < page size")
		}
	})

	t.Run("next_cursor present when exactly page size events returned", func(t *testing.T) {
		triggerStore := &stubTriggerStore{got: newTrigger()}
		eventStore := &stubTriggerEventStore{events: makeFullPage()} // exactly 20
		h := newListTriggerEventsHandler(triggerStore, eventStore)

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
		if _, hasCursor := resp["next_cursor"]; !hasCursor {
			t.Error("expected next_cursor when results == page size")
		}
	})

	t.Run("invalid cursor returns 400", func(t *testing.T) {
		triggerStore := &stubTriggerStore{got: newTrigger()}
		eventStore := &stubTriggerEventStore{}
		h := newListTriggerEventsHandler(triggerStore, eventStore)

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodGet, "/?cursor=not-valid-base64!!!", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("trigger not found returns 404", func(t *testing.T) {
		triggerStore := &stubTriggerStore{getErr: trigger.ErrNotFound}
		eventStore := &stubTriggerEventStore{}
		h := newListTriggerEventsHandler(triggerStore, eventStore)

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "ghost"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "trigger_not_found")
	})

	t.Run("returns empty events array when no events", func(t *testing.T) {
		triggerStore := &stubTriggerStore{got: newTrigger()}
		eventStore := &stubTriggerEventStore{events: []trigger_events.TriggerEvent{}}
		h := newListTriggerEventsHandler(triggerStore, eventStore)

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
		events, ok := resp["events"].([]any)
		if !ok {
			t.Fatalf("expected events array, got %v", resp["events"])
		}
		if len(events) != 0 {
			t.Errorf("expected empty events, got %v", events)
		}
	})
}

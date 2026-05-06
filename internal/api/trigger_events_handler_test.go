package api_test

import (
	"context"
	"encoding/json"
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

func (s *stubTriggerEventStore) Ingest(_ context.Context, _ trigger_events.TriggerEvent) error {
	return nil
}
func (s *stubTriggerEventStore) ListByTrigger(_ context.Context, _ string, _ int) ([]trigger_events.TriggerEvent, error) {
	return s.events, s.listErr
}
func (s *stubTriggerEventStore) TriggerBelongsToDevice(_ context.Context, _, _ string) (bool, error) {
	return true, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newListTriggerEventsHandler(triggerStore trigger.Store, eventStore trigger_events.Store) *api.ListTriggerEventsHandler {
	return &api.ListTriggerEventsHandler{TriggerStore: triggerStore, EventStore: eventStore}
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

	t.Run("returns 200 with events list", func(t *testing.T) {
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
		var resp []map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp) != 1 {
			t.Fatalf("expected 1 event, got %d", len(resp))
		}
		if resp[0]["trigger_event_id"] != "abc123" {
			t.Errorf("trigger_event_id: got %v", resp[0]["trigger_event_id"])
		}
		readings, ok := resp[0]["readings"].([]any)
		if !ok || len(readings) != 1 {
			t.Errorf("expected 1 reading, got %v", resp[0]["readings"])
		}
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

	t.Run("invalid limit returns 400", func(t *testing.T) {
		triggerStore := &stubTriggerStore{got: newTrigger()}
		eventStore := &stubTriggerEventStore{}
		h := newListTriggerEventsHandler(triggerStore, eventStore)

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodGet, "/?limit=abc", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("zero limit returns 400", func(t *testing.T) {
		triggerStore := &stubTriggerStore{got: newTrigger()}
		eventStore := &stubTriggerEventStore{}
		h := newListTriggerEventsHandler(triggerStore, eventStore)

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodGet, "/?limit=0", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("limit above max is clamped to 200", func(t *testing.T) {
		triggerStore := &stubTriggerStore{got: newTrigger()}
		eventStore := &stubTriggerEventStore{events: []trigger_events.TriggerEvent{}}
		h := newListTriggerEventsHandler(triggerStore, eventStore)

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodGet, "/?limit=999", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("returns empty array when no events", func(t *testing.T) {
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
		var resp []any
		json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
		if len(resp) != 0 {
			t.Errorf("expected empty list, got %v", resp)
		}
	})
}

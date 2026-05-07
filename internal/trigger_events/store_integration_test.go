package trigger_events_test

import (
	"context"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
	trigger_events "github.com/fishhub-oss/fishhub-server/internal/trigger_events"
)

func TestTriggerEventsStore_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := trigger_events.NewStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

	var deviceID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO devices (user_id) VALUES ($1) RETURNING id`, userID,
	).Scan(&deviceID); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	var triggerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO triggers (device_id, name, condition, cooldown_s)
		VALUES ($1, 'test trigger', '{}', 60) RETURNING id`, deviceID,
	).Scan(&triggerID); err != nil {
		t.Fatalf("insert trigger: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	t.Run("ingest stores event", func(t *testing.T) {
		err := store.Ingest(ctx, trigger_events.TriggerEvent{
			TriggerEventID: "event-id-1",
			TriggerID:      triggerID,
			FiredAt:        now,
			Readings: []trigger_events.Reading{
				{Peripheral: "ds18b20-4/temperature", Value: 18.5},
			},
		})
		if err != nil {
			t.Fatalf("ingest: %v", err)
		}

		var count int
		db.QueryRowContext(ctx, `SELECT COUNT(*) FROM trigger_events WHERE trigger_event_id = 'event-id-1'`).Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 row, got %d", count)
		}
	})

	t.Run("ingest is idempotent — duplicate trigger_event_id is a no-op", func(t *testing.T) {
		err := store.Ingest(ctx, trigger_events.TriggerEvent{
			TriggerEventID: "event-id-1",
			TriggerID:      triggerID,
			FiredAt:        now,
			Readings:       []trigger_events.Reading{},
		})
		if err != nil {
			t.Fatalf("second ingest: %v", err)
		}

		var count int
		db.QueryRowContext(ctx, `SELECT COUNT(*) FROM trigger_events WHERE trigger_event_id = 'event-id-1'`).Scan(&count)
		if count != 1 {
			t.Errorf("expected exactly 1 row after duplicate ingest, got %d", count)
		}
	})

	t.Run("ListByTriggerCursor first page returns events in fired_at DESC order", func(t *testing.T) {
		earlier := now.Add(-5 * time.Minute)
		if err := store.Ingest(ctx, trigger_events.TriggerEvent{
			TriggerEventID: "event-id-2",
			TriggerID:      triggerID,
			FiredAt:        earlier,
			Readings:       []trigger_events.Reading{},
		}); err != nil {
			t.Fatalf("ingest second event: %v", err)
		}

		events, err := store.ListByTriggerCursor(ctx, triggerID, trigger_events.CursorPage{PageSize: 10})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(events) < 2 {
			t.Fatalf("expected at least 2 events, got %d", len(events))
		}
		if !events[0].FiredAt.After(events[1].FiredAt) {
			t.Errorf("expected events ordered fired_at DESC: first=%v second=%v",
				events[0].FiredAt, events[1].FiredAt)
		}
	})

	t.Run("ListByTriggerCursor respects page size", func(t *testing.T) {
		events, err := store.ListByTriggerCursor(ctx, triggerID, trigger_events.CursorPage{PageSize: 1})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(events) != 1 {
			t.Errorf("expected 1 event with page size 1, got %d", len(events))
		}
	})

	t.Run("ListByTriggerCursor second page excludes first page events", func(t *testing.T) {
		// First page: newest event (fired_at = now).
		first, err := store.ListByTriggerCursor(ctx, triggerID, trigger_events.CursorPage{PageSize: 1})
		if err != nil {
			t.Fatalf("first page: %v", err)
		}
		if len(first) != 1 {
			t.Fatalf("expected 1 event on first page, got %d", len(first))
		}

		// Second page using cursor from the first page's last item.
		afterFiredAt := first[0].FiredAt
		afterID := first[0].ID
		second, err := store.ListByTriggerCursor(ctx, triggerID, trigger_events.CursorPage{
			PageSize:     10,
			AfterFiredAt: &afterFiredAt,
			AfterID:      &afterID,
		})
		if err != nil {
			t.Fatalf("second page: %v", err)
		}
		if len(second) == 0 {
			t.Fatal("expected at least 1 event on second page")
		}
		// All second-page events must be strictly older than the cursor.
		for _, e := range second {
			if !e.FiredAt.Before(afterFiredAt) {
				t.Errorf("second page event fired_at %v is not before cursor %v", e.FiredAt, afterFiredAt)
			}
		}
	})

	t.Run("ListByTriggerCursor returns readings correctly", func(t *testing.T) {
		events, err := store.ListByTriggerCursor(ctx, triggerID, trigger_events.CursorPage{PageSize: 10})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		// event-id-1 is the most recent (fired_at = now)
		if len(events[0].Readings) != 1 {
			t.Fatalf("expected 1 reading on most recent event, got %d", len(events[0].Readings))
		}
		if events[0].Readings[0].Peripheral != "ds18b20-4/temperature" {
			t.Errorf("peripheral: got %q", events[0].Readings[0].Peripheral)
		}
		if events[0].Readings[0].Value != 18.5 {
			t.Errorf("value: got %v", events[0].Readings[0].Value)
		}
	})

	t.Run("TriggerBelongsToDevice returns true for correct device", func(t *testing.T) {
		ok, err := store.TriggerBelongsToDevice(ctx, triggerID, deviceID)
		if err != nil {
			t.Fatalf("belongs: %v", err)
		}
		if !ok {
			t.Error("expected true, got false")
		}
	})

	t.Run("TriggerBelongsToDevice returns false for wrong device", func(t *testing.T) {
		ok, err := store.TriggerBelongsToDevice(ctx, triggerID, "00000000-0000-0000-0000-000000000099")
		if err != nil {
			t.Fatalf("belongs: %v", err)
		}
		if ok {
			t.Error("expected false, got true")
		}
	})
}

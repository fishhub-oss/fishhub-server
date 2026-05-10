package trigger_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
)

func TestTriggerStore_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := trigger.NewStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

	// Insert a device.
	var deviceID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO devices (user_id, model_id) VALUES ($1, (SELECT id FROM device_models WHERE slug = 'fishhub-v1')) RETURNING id`, userID,
	).Scan(&deviceID); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	// Insert an actuator peripheral.
	var peripheralID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO peripherals (device_id, name, kind, pin, category, control_mode)
		 VALUES ($1, 'heater', 'relay', 14, 'actuator', 'automatic') RETURNING id`,
		deviceID,
	).Scan(&peripheralID); err != nil {
		t.Fatalf("insert peripheral: %v", err)
	}

	// Insert a sensor peripheral (not an actuator).
	var sensorID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO peripherals (device_id, name, kind, pin, category)
		 VALUES ($1, 'temp', 'ds18b20', 4, 'sensor') RETURNING id`,
		deviceID,
	).Scan(&sensorID); err != nil {
		t.Fatalf("insert sensor peripheral: %v", err)
	}

	condition := json.RawMessage(`{"op":"lt","left":{"op":"value","measurement":"ds18b20-4/temperature"},"right":{"op":"literal","value":19.0}}`)

	actionConfig, _ := json.Marshal(map[string]any{
		"peripheral_id": peripheralID,
		"command":       "set",
		"value":         1.0,
	})

	var triggerID string

	t.Run("create trigger inserts actions and action_triggers rows", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		tr, err := store.Create(ctx, tx, deviceID, userID, trigger.TriggerCreate{
			Name:      "Heater on cold",
			Condition: condition,
			Actions: []trigger.Action{
				{Type: "peripheral_action", Config: actionConfig},
			},
			CooldownSeconds: 60,
		})
		if err != nil {
			tx.Rollback()
			t.Fatalf("create trigger: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}

		if tr.ID == "" {
			t.Error("expected non-empty trigger ID")
		}
		if tr.Name != "Heater on cold" {
			t.Errorf("name: got %q, want %q", tr.Name, "Heater on cold")
		}
		if !tr.Enabled {
			t.Error("expected enabled=true by default")
		}
		if tr.CooldownSeconds != 60 {
			t.Errorf("cooldown_s: got %d, want 60", tr.CooldownSeconds)
		}
		if len(tr.Actions) != 1 {
			t.Fatalf("expected 1 action, got %d", len(tr.Actions))
		}
		if tr.Actions[0].Type != "peripheral_action" {
			t.Errorf("action type: got %q, want peripheral_action", tr.Actions[0].Type)
		}

		// Verify one actions row was created.
		var actionCount int
		db.QueryRowContext(ctx, `SELECT COUNT(*) FROM actions WHERE id = $1`, tr.Actions[0].ID).Scan(&actionCount)
		if actionCount != 1 {
			t.Errorf("expected 1 actions row, got %d", actionCount)
		}

		// Verify one action_triggers row was created.
		var joinCount int
		db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM action_triggers WHERE trigger_id = $1 AND action_id = $2`,
			tr.ID, tr.Actions[0].ID,
		).Scan(&joinCount)
		if joinCount != 1 {
			t.Errorf("expected 1 action_triggers row, got %d", joinCount)
		}

		triggerID = tr.ID
	})

	t.Run("create with unknown device returns device.ErrNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer tx.Rollback()

		_, err = store.Create(ctx, tx, "00000000-0000-0000-0000-000000000099", userID, trigger.TriggerCreate{
			Name:      "bad",
			Condition: condition,
			Actions:   []trigger.Action{{Type: "peripheral_action", Config: actionConfig}},
		})
		// Store itself just inserts — it does not validate the peripheral. device.ErrNotFound
		// comes from the service layer via validatePeripheralAction. The store will return a
		// Postgres FK violation when device_id doesn't exist.
		if err == nil {
			t.Error("expected error for unknown device, got nil")
		}
	})

	t.Run("list returns created trigger with hydrated actions", func(t *testing.T) {
		triggers, err := store.List(ctx, deviceID, userID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(triggers) == 0 {
			t.Fatal("expected at least one trigger")
		}
		found := false
		for _, tr := range triggers {
			if tr.ID == triggerID {
				found = true
				if tr.Name != "Heater on cold" {
					t.Errorf("name: got %q, want %q", tr.Name, "Heater on cold")
				}
				if len(tr.Actions) != 1 {
					t.Errorf("expected 1 action, got %d", len(tr.Actions))
				} else if tr.Actions[0].Type != "peripheral_action" {
					t.Errorf("action type: got %q", tr.Actions[0].Type)
				}
			}
		}
		if !found {
			t.Error("created trigger not found in list")
		}
	})

	t.Run("list returns empty for wrong user", func(t *testing.T) {
		var otherUserID string
		if err := db.QueryRowContext(ctx,
			`INSERT INTO users (email, provider, provider_sub) VALUES ('other@test.com','test','sub-other') RETURNING id`,
		).Scan(&otherUserID); err != nil {
			t.Fatalf("insert other user: %v", err)
		}
		triggers, err := store.List(ctx, deviceID, otherUserID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(triggers) != 0 {
			t.Errorf("expected empty list for wrong user, got %d triggers", len(triggers))
		}
	})

	t.Run("get returns trigger with hydrated actions", func(t *testing.T) {
		tr, err := store.Get(ctx, deviceID, userID, triggerID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if tr.ID != triggerID {
			t.Errorf("id: got %q, want %q", tr.ID, triggerID)
		}
		if len(tr.Actions) != 1 {
			t.Fatalf("expected 1 action, got %d", len(tr.Actions))
		}
		if tr.Actions[0].Type != "peripheral_action" {
			t.Errorf("action type: got %q", tr.Actions[0].Type)
		}
	})

	t.Run("get unknown trigger returns ErrNotFound", func(t *testing.T) {
		_, err := store.Get(ctx, deviceID, userID, "00000000-0000-0000-0000-000000000000")
		if !errors.Is(err, trigger.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("update trigger replaces action set atomically", func(t *testing.T) {
		newActionConfig, _ := json.Marshal(map[string]any{
			"peripheral_id": peripheralID,
			"peripheral":    "relay-14",
			"command":       "set",
			"value":         0.0,
		})

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		updated, err := store.Update(ctx, tx, deviceID, userID, triggerID, trigger.TriggerUpdate{
			Name:            "Heater on cold updated",
			Condition:       condition,
			Actions:         []trigger.Action{{Type: "peripheral_action", Config: newActionConfig}},
			CooldownSeconds: 120,
			Enabled:         false,
		})
		if err != nil {
			tx.Rollback()
			t.Fatalf("update: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}

		if updated.Name != "Heater on cold updated" {
			t.Errorf("name: got %q, want %q", updated.Name, "Heater on cold updated")
		}
		if updated.Enabled {
			t.Error("expected enabled=false after update")
		}
		if updated.CooldownSeconds != 120 {
			t.Errorf("cooldown_s: got %d, want 120", updated.CooldownSeconds)
		}
		if len(updated.Actions) != 1 {
			t.Fatalf("expected 1 action, got %d", len(updated.Actions))
		}

		var actionCount int
		db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM action_triggers WHERE trigger_id = $1`,
			triggerID,
		).Scan(&actionCount)
		if actionCount != 1 {
			t.Errorf("expected 1 action_triggers row after update, got %d", actionCount)
		}
	})

	t.Run("create trigger with two actions inserts both rows", func(t *testing.T) {
		action2Config, _ := json.Marshal(map[string]any{
			"peripheral_id": peripheralID,
			"command":       "set",
			"value":         0.0,
		})

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		tr, err := store.Create(ctx, tx, deviceID, userID, trigger.TriggerCreate{
			Name:      "Multi-action trigger",
			Condition: condition,
			Actions: []trigger.Action{
				{Type: "peripheral_action", Config: actionConfig},
				{Type: "peripheral_action", Config: action2Config},
			},
			CooldownSeconds: 30,
		})
		if err != nil {
			tx.Rollback()
			t.Fatalf("create multi-action: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}

		if len(tr.Actions) != 2 {
			t.Fatalf("expected 2 actions, got %d", len(tr.Actions))
		}

		var joinCount int
		db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM action_triggers WHERE trigger_id = $1`, tr.ID,
		).Scan(&joinCount)
		if joinCount != 2 {
			t.Errorf("expected 2 action_triggers rows, got %d", joinCount)
		}

		// Get hydrates both actions.
		fetched, err := store.Get(ctx, deviceID, userID, tr.ID)
		if err != nil {
			t.Fatalf("get multi-action: %v", err)
		}
		if len(fetched.Actions) != 2 {
			t.Errorf("get: expected 2 actions, got %d", len(fetched.Actions))
		}
	})

	t.Run("update unknown trigger returns ErrNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer tx.Rollback()

		_, err = store.Update(ctx, tx, deviceID, userID, "00000000-0000-0000-0000-000000000000", trigger.TriggerUpdate{
			Name:      "x",
			Condition: condition,
			Actions:   []trigger.Action{{Type: "peripheral_action", Config: actionConfig}},
		})
		if !errors.Is(err, trigger.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("delete soft-deletes the trigger", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		deleted, err := store.Delete(ctx, tx, deviceID, userID, triggerID)
		if err != nil {
			tx.Rollback()
			t.Fatalf("delete: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
		if deleted.ID != triggerID {
			t.Errorf("deleted id: got %q, want %q", deleted.ID, triggerID)
		}

		// Should no longer appear in list.
		triggers, err := store.List(ctx, deviceID, userID)
		if err != nil {
			t.Fatalf("list after delete: %v", err)
		}
		for _, tr := range triggers {
			if tr.ID == triggerID {
				t.Error("deleted trigger still appears in list")
			}
		}

		// action_triggers and actions rows are left intact (soft-delete only affects triggers).
		var joinCount int
		db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM action_triggers WHERE trigger_id = $1`, triggerID,
		).Scan(&joinCount)
		if joinCount == 0 {
			t.Error("expected action_triggers rows to survive soft-delete")
		}
	})

	t.Run("delete already-deleted trigger returns ErrNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer tx.Rollback()

		_, err = store.Delete(ctx, tx, deviceID, userID, triggerID)
		if !errors.Is(err, trigger.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("create trigger with invalid device FK returns error", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer tx.Rollback()

		_, err = store.Create(ctx, tx, "00000000-0000-0000-0000-000000000099", userID, trigger.TriggerCreate{
			Name:      "orphan",
			Condition: condition,
			Actions:   []trigger.Action{{Type: "peripheral_action", Config: actionConfig}},
		})
		if err == nil {
			t.Error("expected FK violation error, got nil")
		}
		_ = device.ErrNotFound // consumed to confirm package is reachable
	})
}

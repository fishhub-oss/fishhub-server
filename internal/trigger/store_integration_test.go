package trigger_test

import (
	"context"
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
		`INSERT INTO devices (user_id) VALUES ($1) RETURNING id`, userID,
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

	condition := []byte(`{"op":"lt","left":{"op":"value","measurement":"ds18b20-4/temperature"},"right":{"op":"literal","value":19.0}}`)
	action := []byte(`{"action":"set","value":1.0}`)

	var triggerID string

	t.Run("create trigger returns trigger and kind-pin", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		tr, kindPin, err := store.Create(ctx, tx, deviceID, userID, trigger.TriggerCreate{
			Name:               "Heater on cold",
			Condition:          condition,
			TargetPeripheralID: peripheralID,
			Action:             action,
			CooldownSeconds:    60,
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
		if kindPin != "relay-14" {
			t.Errorf("kind-pin: got %q, want %q", kindPin, "relay-14")
		}
		triggerID = tr.ID
	})

	t.Run("create with sensor peripheral returns ErrInvalidPeripheral", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer tx.Rollback()

		_, _, err = store.Create(ctx, tx, deviceID, userID, trigger.TriggerCreate{
			Name:               "bad",
			Condition:          condition,
			TargetPeripheralID: sensorID,
			Action:             action,
			CooldownSeconds:    60,
		})
		if !errors.Is(err, trigger.ErrInvalidPeripheral) {
			t.Errorf("expected ErrInvalidPeripheral, got %v", err)
		}
	})

	t.Run("create with unknown device returns ErrNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer tx.Rollback()

		_, _, err = store.Create(ctx, tx, "00000000-0000-0000-0000-000000000099", userID, trigger.TriggerCreate{
			Name:               "bad",
			Condition:          condition,
			TargetPeripheralID: peripheralID,
			Action:             action,
		})
		if !errors.Is(err, device.ErrNotFound) {
			t.Errorf("expected device.ErrNotFound, got %v", err)
		}
	})

	t.Run("list returns created trigger", func(t *testing.T) {
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

	t.Run("get returns trigger", func(t *testing.T) {
		tr, err := store.Get(ctx, deviceID, userID, triggerID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if tr.ID != triggerID {
			t.Errorf("id: got %q, want %q", tr.ID, triggerID)
		}
	})

	t.Run("get unknown trigger returns ErrNotFound", func(t *testing.T) {
		_, err := store.Get(ctx, deviceID, userID, "00000000-0000-0000-0000-000000000000")
		if !errors.Is(err, trigger.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("update trigger returns updated fields and kind-pin", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		updated, kindPin, err := store.Update(ctx, tx, deviceID, userID, triggerID, trigger.TriggerUpdate{
			Name:            "Heater on cold updated",
			Condition:       condition,
			Action:          action,
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
		if kindPin != "relay-14" {
			t.Errorf("kind-pin: got %q, want %q", kindPin, "relay-14")
		}
	})

	t.Run("update unknown trigger returns ErrNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		defer tx.Rollback()

		_, _, err = store.Update(ctx, tx, deviceID, userID, "00000000-0000-0000-0000-000000000000", trigger.TriggerUpdate{
			Name:      "x",
			Condition: condition,
			Action:    action,
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
}

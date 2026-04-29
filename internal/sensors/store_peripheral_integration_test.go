package sensors_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

func TestPeripheralStore_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := sensors.NewPeripheralStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

	// insert a device owned by the seed user
	var deviceID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO devices (user_id) VALUES ($1) RETURNING id`, userID,
	).Scan(&deviceID); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	t.Run("create actuator peripheral", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		p, err := store.CreatePeripheral(ctx, tx, deviceID, userID, "light", "relay", "actuator", 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if p.ID == "" {
			t.Error("expected non-empty ID")
		}
		if p.Name != "light" || p.Kind != "relay" || p.Pin != 5 {
			t.Errorf("unexpected peripheral: %+v", p)
		}
		if p.Category != "actuator" {
			t.Errorf("expected category=actuator, got %q", p.Category)
		}
		if p.ControlMode == nil || *p.ControlMode != "automatic" {
			t.Errorf("expected control_mode=automatic, got %v", p.ControlMode)
		}
	})

	t.Run("create sensor peripheral has nil control_mode", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		p, err := store.CreatePeripheral(ctx, tx, deviceID, userID, "temp", "ds18b20", "sensor", 4)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if p.Category != "sensor" {
			t.Errorf("expected category=sensor, got %q", p.Category)
		}
		if p.ControlMode != nil {
			t.Errorf("expected nil control_mode for sensor, got %v", p.ControlMode)
		}
	})

	t.Run("list returns created peripherals with category and control_mode", func(t *testing.T) {
		peripherals, err := store.ListPeripherals(ctx, deviceID, userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(peripherals) == 0 {
			t.Fatal("expected at least one peripheral")
		}
		found := false
		for _, p := range peripherals {
			if p.Name == "light" {
				found = true
				if p.Category != "actuator" {
					t.Errorf("expected actuator, got %q", p.Category)
				}
				if p.ControlMode == nil || *p.ControlMode != "automatic" {
					t.Errorf("expected automatic control_mode, got %v", p.ControlMode)
				}
			}
		}
		if !found {
			t.Error("light peripheral not found in list")
		}
	})

	t.Run("create duplicate name returns ErrPeripheralAlreadyExists", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.CreatePeripheral(ctx, tx, deviceID, userID, "light", "relay", "actuator", 6)
		if !errors.Is(err, sensors.ErrPeripheralAlreadyExists) {
			t.Errorf("expected ErrPeripheralAlreadyExists, got %v", err)
		}
	})

	t.Run("create with duplicate pin returns ErrPeripheralPinInUse", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		// pin 5 is already used by the "light" peripheral created above
		_, err = store.CreatePeripheral(ctx, tx, deviceID, userID, "pump", "relay", "actuator", 5)
		if !errors.Is(err, sensors.ErrPeripheralPinInUse) {
			t.Errorf("expected ErrPeripheralPinInUse, got %v", err)
		}
	})

	t.Run("create with unknown device returns ErrDeviceNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.CreatePeripheral(ctx, tx, "00000000-0000-0000-0000-000000000099", userID, "pump", "relay", "actuator", 7)
		if !errors.Is(err, sensors.ErrDeviceNotFound) {
			t.Errorf("expected ErrDeviceNotFound, got %v", err)
		}
	})

	t.Run("set peripheral schedule", func(t *testing.T) {
		schedule := []sensors.ScheduleWindow{
			{From: "08:00", To: "18:00", Value: 1.0},
		}
		p, err := store.SetPeripheralSchedule(ctx, deviceID, userID, "light", schedule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.Schedule) != 1 || p.Schedule[0].From != "08:00" {
			t.Errorf("unexpected schedule: %+v", p.Schedule)
		}
	})

	t.Run("set schedule on unknown peripheral returns ErrPeripheralNotFound", func(t *testing.T) {
		_, err := store.SetPeripheralSchedule(ctx, deviceID, userID, "nope", nil)
		if !errors.Is(err, sensors.ErrPeripheralNotFound) {
			t.Errorf("expected ErrPeripheralNotFound, got %v", err)
		}
	})

	t.Run("set control mode on actuator", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		p, err := store.SetControlMode(ctx, tx, deviceID, userID, "light", "manual")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if p.ControlMode == nil || *p.ControlMode != "manual" {
			t.Errorf("expected control_mode=manual, got %v", p.ControlMode)
		}
	})

	t.Run("set control mode on sensor returns ErrNotAnActuator", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.SetControlMode(ctx, tx, deviceID, userID, "temp", "manual")
		if !errors.Is(err, sensors.ErrNotAnActuator) {
			t.Errorf("expected ErrNotAnActuator, got %v", err)
		}
	})

	t.Run("set control mode on unknown peripheral returns ErrPeripheralNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.SetControlMode(ctx, tx, deviceID, userID, "ghost", "manual")
		if !errors.Is(err, sensors.ErrPeripheralNotFound) {
			t.Errorf("expected ErrPeripheralNotFound, got %v", err)
		}
	})

	t.Run("list returns empty slice for another user's device", func(t *testing.T) {
		var otherUserID string
		if err := db.QueryRowContext(ctx,
			`INSERT INTO users (email, provider, provider_sub) VALUES ('other@test.com','test','sub-other') RETURNING id`,
		).Scan(&otherUserID); err != nil {
			t.Fatalf("insert other user: %v", err)
		}
		peripherals, err := store.ListPeripherals(ctx, deviceID, otherUserID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(peripherals) != 0 {
			t.Errorf("expected empty list for wrong user, got %d peripherals", len(peripherals))
		}
	})

	t.Run("delete peripheral", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		deleted, err := store.DeletePeripheral(ctx, tx, deviceID, userID, "light")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deleted.Kind != "relay" || deleted.Pin != 5 {
			t.Errorf("expected kind=relay pin=5, got kind=%s pin=%d", deleted.Kind, deleted.Pin)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}

		// should no longer appear in list
		peripherals, err := store.ListPeripherals(ctx, deviceID, userID)
		if err != nil {
			t.Fatalf("list after delete: %v", err)
		}
		for _, p := range peripherals {
			if p.Name == "light" {
				t.Error("deleted peripheral still appears in list")
			}
		}
	})

	t.Run("delete non-existent peripheral returns ErrPeripheralNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.DeletePeripheral(ctx, tx, deviceID, userID, "ghost")
		if !errors.Is(err, sensors.ErrPeripheralNotFound) {
			t.Errorf("expected ErrPeripheralNotFound, got %v", err)
		}
	})

	t.Run("same name can be re-created after soft-delete", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		p, err := store.CreatePeripheral(ctx, tx, deviceID, userID, "light", "relay", "actuator", 5)
		if err != nil {
			tx.Rollback()
			t.Fatalf("re-create after delete: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if p.Name != "light" {
			t.Errorf("expected 'light', got %q", p.Name)
		}
	})
}

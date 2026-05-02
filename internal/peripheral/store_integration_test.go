package peripheral_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

func TestPeripheralStore_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := peripheral.NewStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

	var deviceID string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO devices (user_id) VALUES ($1) RETURNING id`, userID,
	).Scan(&deviceID); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	var lightID, tempID string
	{
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx for light: %v", err)
		}
		p, err := store.CreatePeripheral(ctx, tx, deviceID, userID, "light", "relay", "actuator", 5)
		if err != nil {
			tx.Rollback()
			t.Fatalf("create light: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit light: %v", err)
		}
		lightID = p.ID
	}
	{
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx for temp: %v", err)
		}
		p, err := store.CreatePeripheral(ctx, tx, deviceID, userID, "temp", "ds18b20", "sensor", 4)
		if err != nil {
			tx.Rollback()
			t.Fatalf("create temp: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit temp: %v", err)
		}
		tempID = p.ID
	}

	t.Run("create actuator peripheral", func(t *testing.T) {
		p, err := store.GetPeripheral(ctx, deviceID, userID, lightID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
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
		p, err := store.GetPeripheral(ctx, deviceID, userID, tempID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
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

	t.Run("create duplicate name returns ErrAlreadyExists", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.CreatePeripheral(ctx, tx, deviceID, userID, "light", "relay", "actuator", 6)
		if !errors.Is(err, peripheral.ErrAlreadyExists) {
			t.Errorf("expected ErrAlreadyExists, got %v", err)
		}
	})

	t.Run("create with duplicate pin returns ErrPinInUse", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.CreatePeripheral(ctx, tx, deviceID, userID, "pump", "relay", "actuator", 5)
		if !errors.Is(err, peripheral.ErrPinInUse) {
			t.Errorf("expected ErrPinInUse, got %v", err)
		}
	})

	t.Run("create with unknown device returns ErrNotFound (device)", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.CreatePeripheral(ctx, tx, "00000000-0000-0000-0000-000000000099", userID, "pump", "relay", "actuator", 7)
		if !errors.Is(err, device.ErrNotFound) {
			t.Errorf("expected device.ErrNotFound, got %v", err)
		}
	})

	t.Run("set peripheral schedule", func(t *testing.T) {
		schedule := []peripheral.ScheduleWindow{
			{From: "08:00", To: "18:00", Value: 1.0},
		}
		p, err := store.SetPeripheralSchedule(ctx, deviceID, userID, lightID, schedule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.Schedule) != 1 || p.Schedule[0].From != "08:00" {
			t.Errorf("unexpected schedule: %+v", p.Schedule)
		}
	})

	t.Run("set schedule on unknown peripheral returns ErrNotFound", func(t *testing.T) {
		_, err := store.SetPeripheralSchedule(ctx, deviceID, userID, "00000000-0000-0000-0000-000000000000", nil)
		if !errors.Is(err, peripheral.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("set control mode on actuator", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		p, err := store.SetControlMode(ctx, tx, deviceID, userID, lightID, "manual")
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

		_, err = store.SetControlMode(ctx, tx, deviceID, userID, tempID, "manual")
		if !errors.Is(err, peripheral.ErrNotAnActuator) {
			t.Errorf("expected ErrNotAnActuator, got %v", err)
		}
	})

	t.Run("set control mode on unknown peripheral returns ErrNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.SetControlMode(ctx, tx, deviceID, userID, "00000000-0000-0000-0000-000000000000", "manual")
		if !errors.Is(err, peripheral.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
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

		deleted, err := store.DeletePeripheral(ctx, tx, deviceID, userID, lightID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deleted.Kind != "relay" || deleted.Pin != 5 {
			t.Errorf("expected kind=relay pin=5, got kind=%s pin=%d", deleted.Kind, deleted.Pin)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}

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

	t.Run("delete non-existent peripheral returns ErrNotFound", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()

		_, err = store.DeletePeripheral(ctx, tx, deviceID, userID, "00000000-0000-0000-0000-000000000000")
		if !errors.Is(err, peripheral.ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
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

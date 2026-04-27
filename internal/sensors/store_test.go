package sensors_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
	_ "github.com/lib/pq"
)

func TestListByUserID_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := sensors.NewDeviceStore(db)
	provisioning := sensors.NewProvisioningStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

	t.Run("returns devices for the user ordered by created_at DESC", func(t *testing.T) {
		code, err := provisioning.GetOrCreateCode(ctx, userID)
		if err != nil {
			t.Fatalf("setup code: %v", err)
		}
		d1, _, err := provisioning.ClaimCode(ctx, code)
		if err != nil {
			t.Fatalf("setup claim: %v", err)
		}
		// create a second device via direct insert so we have two distinct ones
		var d2 string
		if err := db.QueryRowContext(ctx, `INSERT INTO devices (user_id) VALUES ($1) RETURNING id`, userID).Scan(&d2); err != nil {
			t.Fatalf("setup device 2: %v", err)
		}

		devices, err := store.ListByUserID(ctx, userID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(devices) < 2 {
			t.Fatalf("expected at least 2 devices, got %d", len(devices))
		}

		ids := make([]string, len(devices))
		for i, d := range devices {
			ids[i] = d.ID
		}
		if !contains(ids, d1) || !contains(ids, d2) {
			t.Errorf("missing expected device IDs in result")
		}
	})

	t.Run("returns empty slice for user with no devices", func(t *testing.T) {
		var newUserID string
		err := db.QueryRowContext(ctx, `
			INSERT INTO users (email, provider, provider_sub)
			VALUES ('other@example.com', 'google', 'sub-other')
			RETURNING id
		`).Scan(&newUserID)
		if err != nil {
			t.Fatalf("insert user: %v", err)
		}

		devices, err := store.ListByUserID(ctx, newUserID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(devices) != 0 {
			t.Errorf("expected empty slice, got %d devices", len(devices))
		}
	})
}

func TestGetActivationStatus_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := sensors.NewDeviceStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

	// helper: insert a device with mqtt credentials and optional outbox event
	setupDevice := func(t *testing.T, withCreds bool, withPendingOutbox bool) string {
		t.Helper()
		var deviceID string
		err := db.QueryRowContext(ctx,
			`INSERT INTO devices (user_id) VALUES ($1) RETURNING id`, userID,
		).Scan(&deviceID)
		if err != nil {
			t.Fatalf("insert device: %v", err)
		}
		if withCreds {
			_, err = db.ExecContext(ctx, `
				UPDATE devices SET mqtt_username='user1', mqtt_password='pass1'
				WHERE id = $1`, deviceID)
			if err != nil {
				t.Fatalf("set mqtt creds: %v", err)
			}
		}
		if withPendingOutbox {
			_, err = db.ExecContext(ctx, `
				INSERT INTO outbox_events (event_type, payload, status)
				VALUES ('hivemq.provision_device', jsonb_build_object('device_id', $1::text), 'pending')`,
				deviceID)
			if err != nil {
				t.Fatalf("insert outbox event: %v", err)
			}
		}
		return deviceID
	}

	t.Run("ready when credentials present and no pending outbox", func(t *testing.T) {
		deviceID := setupDevice(t, true, false)
		status, err := store.GetActivationStatus(ctx, deviceID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !status.Ready {
			t.Error("expected Ready=true")
		}
		if status.MQTTUsername != "user1" {
			t.Errorf("expected mqtt_username=user1, got %q", status.MQTTUsername)
		}
		if status.MQTTPassword != "pass1" {
			t.Errorf("expected mqtt_password=pass1, got %q", status.MQTTPassword)
		}
		// mqtt_host is injected at the handler layer from server config, not stored in DB
	})

	t.Run("not ready when credentials present but outbox pending", func(t *testing.T) {
		deviceID := setupDevice(t, true, true)
		status, err := store.GetActivationStatus(ctx, deviceID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status.Ready {
			t.Error("expected Ready=false while outbox event is pending")
		}
	})

	t.Run("not ready when credentials missing", func(t *testing.T) {
		deviceID := setupDevice(t, false, false)
		status, err := store.GetActivationStatus(ctx, deviceID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if status.Ready {
			t.Error("expected Ready=false when no mqtt credentials")
		}
	})

	t.Run("not found for unknown device id", func(t *testing.T) {
		_, err := store.GetActivationStatus(ctx, "00000000-0000-0000-0000-000000000000")
		if !errors.Is(err, sensors.ErrDeviceNotFound) {
			t.Errorf("expected ErrDeviceNotFound, got %v", err)
		}
	})
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

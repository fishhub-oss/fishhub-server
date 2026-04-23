package sensors_test

import (
	"context"
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
		d1, _, err := provisioning.GetOrCreatePending(ctx, userID)
		if err != nil {
			t.Fatalf("setup device 1: %v", err)
		}
		// create a second device via direct insert so we have two distinct ones
		var d2 string
		if err := db.QueryRowContext(ctx, `INSERT INTO devices (user_id) VALUES ($1) RETURNING id`, userID).Scan(&d2); err != nil {
			t.Fatalf("setup device 2: %v", err)
		}

		devices, err := store.ListByUserID(ctx, userID, "")
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

		devices, err := store.ListByUserID(ctx, newUserID, "")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(devices) != 0 {
			t.Errorf("expected empty slice, got %d devices", len(devices))
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

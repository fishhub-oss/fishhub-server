package sensors_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

func TestGetOrCreatePending_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := sensors.NewProvisioningStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

	t.Run("creates a pending device and code on first call", func(t *testing.T) {
		deviceID, code, err := store.GetOrCreatePending(ctx, userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deviceID == "" {
			t.Error("expected non-empty device_id")
		}
		if len(code) != 6 {
			t.Errorf("expected 6-char code, got %q", code)
		}
	})

	t.Run("second call returns same device and code", func(t *testing.T) {
		var uid string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (email, provider, provider_sub)
			VALUES ('idempotent@test.com', 'test', 'sub-idempotent')
			RETURNING id`,
		).Scan(&uid); err != nil {
			t.Fatalf("insert user: %v", err)
		}

		deviceID1, code1, err := store.GetOrCreatePending(ctx, uid)
		if err != nil {
			t.Fatalf("first call: %v", err)
		}
		deviceID2, code2, err := store.GetOrCreatePending(ctx, uid)
		if err != nil {
			t.Fatalf("second call: %v", err)
		}
		if deviceID1 != deviceID2 {
			t.Errorf("expected same device_id, got %s and %s", deviceID1, deviceID2)
		}
		if code1 != code2 {
			t.Errorf("expected same code, got %s and %s", code1, code2)
		}
	})
}

func TestClaimCode_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := sensors.NewProvisioningStore(db)
	ctx := context.Background()

	insertUser := func(t *testing.T, suffix string) string {
		t.Helper()
		var uid string
		if err := db.QueryRowContext(ctx, fmt.Sprintf(`
			INSERT INTO users (email, provider, provider_sub)
			VALUES ('claim-%s@test.com', 'test', 'sub-claim-%s')
			RETURNING id`, suffix, suffix),
		).Scan(&uid); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		return uid
	}

	t.Run("claims code and returns device_id", func(t *testing.T) {
		userID := insertUser(t, "a")
		deviceID, code, err := store.GetOrCreatePending(ctx, userID)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		claimedDeviceID, _, err := store.ClaimCode(ctx, code)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if claimedDeviceID != deviceID {
			t.Errorf("expected device_id %s, got %s", deviceID, claimedDeviceID)
		}
	})

	t.Run("claiming same code twice returns ErrCodeAlreadyUsed", func(t *testing.T) {
		userID := insertUser(t, "b")
		_, code, err := store.GetOrCreatePending(ctx, userID)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		if _, _, err := store.ClaimCode(ctx, code); err != nil {
			t.Fatalf("first claim: %v", err)
		}

		_, _, err = store.ClaimCode(ctx, code)
		if !errors.Is(err, sensors.ErrCodeAlreadyUsed) {
			t.Errorf("expected ErrCodeAlreadyUsed, got %v", err)
		}
	})

	t.Run("unknown code returns ErrCodeNotFound", func(t *testing.T) {
		_, _, err := store.ClaimCode(ctx, "XXXXXX")
		if !errors.Is(err, sensors.ErrCodeNotFound) {
			t.Errorf("expected ErrCodeNotFound, got %v", err)
		}
	})
}

func TestActivate_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := sensors.NewProvisioningStore(db)
	deviceStore := sensors.NewDeviceStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

	t.Run("sets device status to active", func(t *testing.T) {
		deviceID, code, err := store.GetOrCreatePending(ctx, userID)
		if err != nil {
			t.Fatalf("setup provision: %v", err)
		}
		if _, _, err := store.ClaimCode(ctx, code); err != nil {
			t.Fatalf("setup claim: %v", err)
		}

		if err := store.Activate(ctx, deviceID); err != nil {
			t.Fatalf("activate: %v", err)
		}

		devices, err := deviceStore.ListByUserID(ctx, userID, "active")
		if err != nil {
			t.Fatalf("list active: %v", err)
		}
		found := false
		for _, d := range devices {
			if d.ID == deviceID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected device %s to be active", deviceID)
		}
	})

	t.Run("active device appears in ListByUserID with status filter", func(t *testing.T) {
		var uid string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (email, provider, provider_sub)
			VALUES ('activate-list@test.com', 'test', 'sub-activate-list')
			RETURNING id`,
		).Scan(&uid); err != nil {
			t.Fatalf("insert user: %v", err)
		}

		deviceID, code, err := store.GetOrCreatePending(ctx, uid)
		if err != nil {
			t.Fatalf("setup provision: %v", err)
		}
		if _, _, err := store.ClaimCode(ctx, code); err != nil {
			t.Fatalf("setup claim: %v", err)
		}
		if err := store.Activate(ctx, deviceID); err != nil {
			t.Fatalf("activate: %v", err)
		}

		active, err := deviceStore.ListByUserID(ctx, uid, "active")
		if err != nil {
			t.Fatalf("list active: %v", err)
		}
		found := false
		for _, d := range active {
			if d.ID == deviceID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected activated device %s in active list", deviceID)
		}

		_, _, err = store.GetOrCreatePending(ctx, uid)
		if err != nil {
			t.Fatalf("create pending: %v", err)
		}
		active2, err := deviceStore.ListByUserID(ctx, uid, "active")
		if err != nil {
			t.Fatalf("list active after pending: %v", err)
		}
		if len(active2) != len(active) {
			t.Errorf("pending device leaked into active list (before: %d, after: %d)", len(active), len(active2))
		}
	})
}

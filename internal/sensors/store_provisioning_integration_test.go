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

func TestGetOrCreateCode_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := sensors.NewProvisioningStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

	t.Run("creates a code on first call", func(t *testing.T) {
		code, err := store.GetOrCreateCode(ctx, userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(code) != 6 {
			t.Errorf("expected 6-char code, got %q", code)
		}
	})

	t.Run("second call returns same code", func(t *testing.T) {
		var uid string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (email, provider, provider_sub)
			VALUES ('idempotent@test.com', 'test', 'sub-idempotent')
			RETURNING id`,
		).Scan(&uid); err != nil {
			t.Fatalf("insert user: %v", err)
		}

		code1, err := store.GetOrCreateCode(ctx, uid)
		if err != nil {
			t.Fatalf("first call: %v", err)
		}
		code2, err := store.GetOrCreateCode(ctx, uid)
		if err != nil {
			t.Fatalf("second call: %v", err)
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

	t.Run("claims code and creates device row", func(t *testing.T) {
		userID := insertUser(t, "a")
		code, err := store.GetOrCreateCode(ctx, userID)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		deviceID, claimedUserID, err := store.ClaimCode(ctx, code)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deviceID == "" {
			t.Error("expected non-empty device_id")
		}
		if claimedUserID != userID {
			t.Errorf("expected user_id %s, got %s", userID, claimedUserID)
		}
	})

	t.Run("claiming same code twice returns ErrCodeAlreadyUsed", func(t *testing.T) {
		userID := insertUser(t, "b")
		code, err := store.GetOrCreateCode(ctx, userID)
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

	t.Run("sets mqtt credentials on device", func(t *testing.T) {
		code, err := store.GetOrCreateCode(ctx, userID)
		if err != nil {
			t.Fatalf("setup provision: %v", err)
		}
		deviceID, _, err := store.ClaimCode(ctx, code)
		if err != nil {
			t.Fatalf("setup claim: %v", err)
		}

		if err := store.Activate(ctx, deviceID, "mqtt-user", "mqtt-pass"); err != nil {
			t.Fatalf("activate: %v", err)
		}

		devices, err := deviceStore.ListByUserID(ctx, userID)
		if err != nil {
			t.Fatalf("list devices: %v", err)
		}
		found := false
		for _, d := range devices {
			if d.ID == deviceID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected device %s to appear in list", deviceID)
		}
	})

	t.Run("new code can be created after claim", func(t *testing.T) {
		var uid string
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (email, provider, provider_sub)
			VALUES ('activate-new@test.com', 'test', 'sub-activate-new')
			RETURNING id`,
		).Scan(&uid); err != nil {
			t.Fatalf("insert user: %v", err)
		}

		code1, err := store.GetOrCreateCode(ctx, uid)
		if err != nil {
			t.Fatalf("setup provision: %v", err)
		}
		if _, _, err := store.ClaimCode(ctx, code1); err != nil {
			t.Fatalf("setup claim: %v", err)
		}

		// after claim, user can get a fresh code
		code2, err := store.GetOrCreateCode(ctx, uid)
		if err != nil {
			t.Fatalf("second provision: %v", err)
		}
		if code2 == code1 {
			t.Error("expected a new code after the first was claimed")
		}
	})
}

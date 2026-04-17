package store_test

import (
	"context"
	"errors"
	"testing"

	appdb "github.com/fishhub-oss/fishhub-server/internal/db"
	"github.com/fishhub-oss/fishhub-server/internal/store"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
	_ "github.com/lib/pq"
)

func TestLookupByToken_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	devices := store.NewDeviceStore(db)
	tokens := store.NewTokenStore(db)
	ctx := context.Background()
	userID := appdb.SeedUserID()

	t.Run("returns device info for valid token", func(t *testing.T) {
		result, err := tokens.CreateToken(ctx, userID)
		if err != nil {
			t.Fatalf("setup: create token: %v", err)
		}

		info, err := devices.LookupByToken(ctx, result.Token)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.DeviceID != result.DeviceID {
			t.Errorf("expected device_id %s, got %s", result.DeviceID, info.DeviceID)
		}
		if info.UserID != userID {
			t.Errorf("expected user_id %s, got %s", userID, info.UserID)
		}
	})

	t.Run("returns ErrTokenNotFound for unknown token", func(t *testing.T) {
		_, err := devices.LookupByToken(ctx, "0000000000000000000000000000000000000000000000000000000000000000")
		if !errors.Is(err, store.ErrTokenNotFound) {
			t.Errorf("expected ErrTokenNotFound, got %v", err)
		}
	})
}

package sensors_test

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
	_ "github.com/lib/pq"
)

func TestLookupByToken_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	devices := sensors.NewDeviceStore(db)
	tokens := sensors.NewTokenStore(db)
	ctx := context.Background()
	userID := platform.SeedUserID()

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
		if !errors.Is(err, sensors.ErrTokenNotFound) {
			t.Errorf("expected ErrTokenNotFound, got %v", err)
		}
	})
}

func TestCreateToken_integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	s := sensors.NewTokenStore(db)
	userID := platform.SeedUserID()
	ctx := context.Background()

	t.Run("returns valid token and IDs", func(t *testing.T) {
		result, err := s.CreateToken(ctx, userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.UserID != userID {
			t.Errorf("expected user_id %s, got %s", userID, result.UserID)
		}
		if result.DeviceID == "" {
			t.Error("expected non-empty device_id")
		}
		if len(result.Token) != 64 {
			t.Errorf("expected 64-char token, got %d", len(result.Token))
		}
		if _, err := hex.DecodeString(result.Token); err != nil {
			t.Errorf("token is not valid hex: %v", err)
		}
	})

	t.Run("persists device and token to DB", func(t *testing.T) {
		result, err := s.CreateToken(ctx, userID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var deviceUserID string
		var deviceName *string
		err = db.QueryRowContext(ctx,
			`SELECT user_id, name FROM devices WHERE id = $1`, result.DeviceID,
		).Scan(&deviceUserID, &deviceName)
		if err != nil {
			t.Fatalf("device not found in DB: %v", err)
		}
		if deviceUserID != userID {
			t.Errorf("device has wrong user_id: %s", deviceUserID)
		}
		if deviceName != nil {
			t.Errorf("expected name to be NULL, got %s", *deviceName)
		}

		var storedToken string
		err = db.QueryRowContext(ctx,
			`SELECT token FROM device_tokens WHERE device_id = $1`, result.DeviceID,
		).Scan(&storedToken)
		if err != nil {
			t.Fatalf("token not found in DB: %v", err)
		}
		if storedToken != result.Token {
			t.Errorf("stored token mismatch: got %s", storedToken)
		}
	})

	t.Run("each call creates a distinct device and token", func(t *testing.T) {
		r1, err := s.CreateToken(ctx, userID)
		if err != nil {
			t.Fatalf("first call error: %v", err)
		}
		r2, err := s.CreateToken(ctx, userID)
		if err != nil {
			t.Fatalf("second call error: %v", err)
		}
		if r1.DeviceID == r2.DeviceID {
			t.Error("expected distinct device IDs")
		}
		if r1.Token == r2.Token {
			t.Error("expected distinct tokens")
		}
	})

	t.Run("invalid userID returns error and nothing is committed", func(t *testing.T) {
		_, err := s.CreateToken(ctx, "00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Fatal("expected error for unknown user_id, got nil")
		}

		var count int
		db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM devices WHERE user_id = '00000000-0000-0000-0000-000000000000'`,
		).Scan(&count)
		if count != 0 {
			t.Errorf("expected no orphan devices, found %d", count)
		}
	})
}

var _ sensors.TokenStore = sensors.NewTokenStore((*sql.DB)(nil))

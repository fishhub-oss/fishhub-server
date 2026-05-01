package sensors

import (
	"context"
	"database/sql"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
)

// DeviceStore is an alias for device.Store kept for backward compatibility within this package.
type DeviceStore = device.Store

// PeripheralStore is an alias for peripheral.Store kept for backward compatibility within this package.
type PeripheralStore = peripheral.Store

type ProvisioningStore interface {
	// GetOrCreateCode returns the existing unused code for the user, or creates one.
	GetOrCreateCode(ctx context.Context, userID string) (code string, err error)
	// ClaimCode marks the code used, creates a new device row, and returns the device ID and user ID.
	// Returns ErrCodeNotFound if the code is unknown, ErrCodeAlreadyUsed if already claimed.
	ClaimCode(ctx context.Context, code string) (deviceID, userID string, err error)
	// Activate stores MQTT credentials on the device row within the provided transaction.
	// The caller owns the transaction boundary.
	Activate(ctx context.Context, tx *sql.Tx, deviceID, mqttUsername, mqttPassword string) error
}

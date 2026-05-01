package sensors

import (
	"context"
	"database/sql"

	"github.com/fishhub-oss/fishhub-server/internal/device"
)

// DeviceStore is an alias for device.Store kept for backward compatibility within this package.
type DeviceStore = device.Store

type PeripheralStore interface {
	// CreatePeripheral inserts a new peripheral for the device owned by userID.
	// Returns ErrDeviceNotFound if the device does not exist or is not owned by userID.
	// Returns ErrPeripheralAlreadyExists if an active peripheral with the same name exists.
	// Returns ErrPeripheralPinInUse if an active peripheral with the same pin exists.
	// category must be "sensor" or "actuator"; actuators get control_mode defaulted to "automatic".
	CreatePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, name, kind, category string, pin int) (Peripheral, error)
	// ListPeripherals returns active (non-deleted) peripherals for the device owned by userID.
	// Returns an empty slice if the device does not exist or is not owned by userID.
	ListPeripherals(ctx context.Context, deviceID, userID string) ([]Peripheral, error)
	// GetPeripheral returns a single active peripheral by ID, scoped to the device and user.
	// Returns ErrPeripheralNotFound if it does not exist or is not reachable by userID.
	GetPeripheral(ctx context.Context, deviceID, userID, peripheralID string) (Peripheral, error)
	// SetPeripheralSchedule persists the schedule and returns the updated peripheral.
	// Returns ErrPeripheralNotFound if the peripheral does not exist or is not reachable by userID.
	SetPeripheralSchedule(ctx context.Context, deviceID, userID, peripheralID string, schedule []ScheduleWindow) (Peripheral, error)
	// SetControlMode updates control_mode for an actuator peripheral within the provided transaction.
	// Returns ErrPeripheralNotFound if the peripheral does not exist or is not reachable by userID.
	// Returns ErrNotAnActuator if the peripheral's category is not "actuator".
	SetControlMode(ctx context.Context, tx *sql.Tx, deviceID, userID, peripheralID, mode string) (Peripheral, error)
	// DeletePeripheral soft-deletes the peripheral (sets deleted_at) and returns it.
	// Returns ErrPeripheralNotFound if the peripheral does not exist or is not reachable by userID.
	DeletePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, peripheralID string) (Peripheral, error)
}

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

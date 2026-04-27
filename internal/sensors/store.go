package sensors

import (
	"context"
	"database/sql"
	"time"
)

type Device struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// ActivationStatus holds the device's MQTT readiness state.
type ActivationStatus struct {
	Ready        bool
	MQTTUsername string
	MQTTPassword string
	MQTTHost     string
	MQTTPort     int
}

type DeviceStore interface {
	ListByUserID(ctx context.Context, userID string) ([]Device, error)
	FindByIDAndUserID(ctx context.Context, deviceID, userID string) (Device, error)
	// PatchDevice updates the name of the device owned by userID.
	// Returns ErrDeviceNotFound if the device does not exist or is not owned by the user.
	PatchDevice(ctx context.Context, deviceID, userID, name string) (Device, error)
	// DeleteDevice soft-deletes the device and returns its mqtt_username for cleanup.
	// Returns ErrDeviceNotFound if the device does not exist or is not owned by the user.
	DeleteDevice(ctx context.Context, deviceID, userID string) (mqttUsername string, err error)
	// GetActivationStatus returns whether the device's MQTT credentials are ready.
	// Ready = credentials present in DB AND no pending/processing outbox event for the device.
	// Returns ErrDeviceNotFound if the device does not exist.
	GetActivationStatus(ctx context.Context, deviceID string) (ActivationStatus, error)
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

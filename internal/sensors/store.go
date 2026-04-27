package sensors

import (
	"context"
	"time"
)

type Device struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

type DeviceStore interface {
	// ListByUserID returns all devices owned by userID. The status parameter is accepted
	// for backwards compatibility but is ignored — all rows in devices are active by definition.
	ListByUserID(ctx context.Context, userID, status string) ([]Device, error)
	FindByIDAndUserID(ctx context.Context, deviceID, userID string) (Device, error)
	// PatchDevice updates the name of the device owned by userID.
	// Returns ErrDeviceNotFound if the device does not exist or is not owned by the user.
	PatchDevice(ctx context.Context, deviceID, userID, name string) (Device, error)
	// DeleteDevice soft-deletes the device and returns its mqtt_username for cleanup.
	// Returns ErrDeviceNotFound if the device does not exist or is not owned by the user.
	DeleteDevice(ctx context.Context, deviceID, userID string) (mqttUsername string, err error)
}

type ProvisioningStore interface {
	// GetOrCreateCode returns the existing unused code for the user, or creates one.
	GetOrCreateCode(ctx context.Context, userID string) (code string, err error)
	// ClaimCode marks the code used, creates a new device row, and returns the device ID and user ID.
	// Returns ErrCodeNotFound if the code is unknown, ErrCodeAlreadyUsed if already claimed.
	ClaimCode(ctx context.Context, code string) (deviceID, userID string, err error)
	// Activate stores MQTT credentials on the device row.
	Activate(ctx context.Context, deviceID, mqttUsername, mqttPassword string) error
}

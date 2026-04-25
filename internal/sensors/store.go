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
	// ListByUserID returns devices owned by userID. If status is non-empty, only
	// devices with that status are returned.
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
	// GetOrCreatePending returns the existing pending device + code for the user,
	// or creates both atomically if none exists.
	GetOrCreatePending(ctx context.Context, userID string) (deviceID, code string, err error)
	// ClaimCode marks the code as used and returns the associated device ID and user ID.
	// Returns ErrCodeNotFound if the code is unknown, ErrCodeAlreadyUsed if already claimed.
	ClaimCode(ctx context.Context, code string) (deviceID, userID string, err error)
	// Activate sets the device status to active and stores MQTT credentials.
	Activate(ctx context.Context, deviceID, mqttUsername, mqttPassword string) error
}

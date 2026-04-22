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
	LookupByToken(ctx context.Context, token string) (DeviceInfo, error)
	// ListByUserID returns devices owned by userID. If status is non-empty, only
	// devices with that status are returned.
	ListByUserID(ctx context.Context, userID, status string) ([]Device, error)
	FindByIDAndUserID(ctx context.Context, deviceID, userID string) (Device, error)
}

type TokenStore interface {
	CreateToken(ctx context.Context, userID string) (TokenResult, error)
}

type ProvisioningStore interface {
	// GetOrCreatePending returns the existing pending device + code for the user,
	// or creates both atomically if none exists.
	GetOrCreatePending(ctx context.Context, userID string) (deviceID, code string, err error)
	// ClaimCode marks the code as used and returns the associated device ID.
	// Returns ErrCodeNotFound if the code is unknown, ErrCodeAlreadyUsed if already claimed.
	ClaimCode(ctx context.Context, code string) (deviceID string, err error)
	// Activate writes the Bearer token into device_tokens and sets the device status to active.
	Activate(ctx context.Context, deviceID, token string) error
}

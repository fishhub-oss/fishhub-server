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
	ListByUserID(ctx context.Context, userID string) ([]Device, error)
	FindByIDAndUserID(ctx context.Context, deviceID, userID string) (Device, error)
}

type TokenStore interface {
	CreateToken(ctx context.Context, userID string) (TokenResult, error)
}

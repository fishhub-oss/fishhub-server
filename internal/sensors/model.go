package sensors

import (
	"context"
	"errors"
)

var ErrDeviceNotFound  = errors.New("device not found")
var ErrCodeNotFound    = errors.New("provisioning code not found")
var ErrCodeAlreadyUsed = errors.New("provisioning code already used")
var ErrInvalidCommand  = errors.New("action must be 'set' or 'schedule'")
var ErrInfluxWrite     = errors.New("failed to persist reading")

type DeviceInfo struct {
	DeviceID string
	UserID   string
}

type contextKey string

const DeviceContextKey contextKey = "device"

func DeviceFromContext(ctx context.Context) (DeviceInfo, bool) {
	info, ok := ctx.Value(DeviceContextKey).(DeviceInfo)
	return info, ok
}

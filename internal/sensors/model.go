package sensors

import (
	"context"
	"errors"
)

var ErrTokenNotFound    = errors.New("token not found")
var ErrDeviceNotFound   = errors.New("device not found")
var ErrCodeNotFound     = errors.New("provisioning code not found")
var ErrCodeAlreadyUsed  = errors.New("provisioning code already used")

type DeviceInfo struct {
	DeviceID string
	UserID   string
}

type TokenResult struct {
	Token    string
	DeviceID string
	UserID   string
}

type contextKey string

const DeviceContextKey contextKey = "device"

func DeviceFromContext(ctx context.Context) (DeviceInfo, bool) {
	info, ok := ctx.Value(DeviceContextKey).(DeviceInfo)
	return info, ok
}

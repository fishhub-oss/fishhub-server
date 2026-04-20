package sensors

import (
	"context"
	"errors"
)

var ErrTokenNotFound = errors.New("token not found")

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

package device

import (
	"context"
	"time"
)

type Device struct {
	ID        string
	UserID    string
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

// Info carries the device identity stored in the request context by the device auth middleware.
type Info struct {
	DeviceID string
	UserID   string
}

type contextKey string

const ContextKey contextKey = "device"

func FromContext(ctx context.Context) (Info, bool) {
	info, ok := ctx.Value(ContextKey).(Info)
	return info, ok
}

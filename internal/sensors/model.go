package sensors

import (
	"context"
	"errors"
	"time"
)

type Peripheral struct {
	ID        string
	DeviceID  string
	Name      string
	Kind      string
	Pin       int
	Schedule  []ScheduleWindow
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ScheduleWindow struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	Value float64 `json:"value"`
	Days  []int   `json:"days,omitempty"`
}

var ErrDeviceNotFound          = errors.New("device not found")
var ErrCodeNotFound            = errors.New("provisioning code not found")
var ErrCodeAlreadyUsed         = errors.New("provisioning code already used")
var ErrInvalidCommand          = errors.New("action must be 'set' or 'schedule'")
var ErrInfluxWrite             = errors.New("failed to persist reading")
var ErrPeripheralNotFound      = errors.New("peripheral not found")
var ErrPeripheralAlreadyExists = errors.New("peripheral already exists")
var ErrPeripheralPinInUse      = errors.New("peripheral pin already in use")

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

package sensors

import (
	"errors"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/device"
)

type Peripheral struct {
	ID          string
	DeviceID    string
	Name        string
	Kind        string
	Pin         int
	Category    string  // "sensor" | "actuator"
	ControlMode *string // nil for sensors; "automatic"|"manual" for actuators
	Schedule    []ScheduleWindow
	LastReading *ReadingPoint
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ScheduleWindow struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	Value float64 `json:"value"`
	Days  []int   `json:"days,omitempty"`
}

var ErrNotAnActuator           = errors.New("peripheral is not an actuator")
var ErrDeviceNotFound          = device.ErrNotFound
var ErrCodeNotFound            = errors.New("provisioning code not found")
var ErrCodeAlreadyUsed         = errors.New("provisioning code already used")
var ErrInvalidCommand          = errors.New("action must be 'set' or 'schedule'")
var ErrInfluxWrite             = errors.New("failed to persist reading")
var ErrPeripheralNotFound      = errors.New("peripheral not found")
var ErrPeripheralAlreadyExists = errors.New("peripheral already exists")
var ErrPeripheralPinInUse      = errors.New("peripheral pin already in use")

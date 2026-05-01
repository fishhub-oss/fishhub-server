package peripheral

import (
	"errors"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/measurement"
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
	LastReading *measurement.Point
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ScheduleWindow struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	Value float64 `json:"value"`
	Days  []int   `json:"days,omitempty"`
}

var (
	ErrNotAnActuator  = errors.New("peripheral is not an actuator")
	ErrNotFound       = errors.New("peripheral not found")
	ErrAlreadyExists  = errors.New("peripheral already exists")
	ErrPinInUse       = errors.New("peripheral pin already in use")
	ErrInvalidCommand = errors.New("action must be 'set' or 'schedule'")
)

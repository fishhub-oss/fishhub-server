package peripheral

import (
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

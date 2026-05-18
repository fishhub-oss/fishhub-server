package peripheral

import (
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/measurement"
)

// Port is the resolved port information carried on a Peripheral after a join.
type Port struct {
	ID    string
	Label string
	Pin   int
}

type Peripheral struct {
	ID          string
	DeviceID    string
	Name        string
	Kind        string
	Pin         int
	Port        *Port   // nil for peripherals that pre-date the port model
	Category    string  // "sensor" | "actuator"
	ControlMode *string // nil for sensors; "automatic"|"manual" for actuators
	Purpose     *string
	Schedule    *Schedule
	LastReading *measurement.Point
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Schedule struct {
	Type    string           `json:"type"` // "windows" | "cron"
	Windows []ScheduleWindow `json:"windows,omitempty"`
	Entries []CronEntry      `json:"entries,omitempty"`
}

type ScheduleWindow struct {
	From  string  `json:"from"`
	To    string  `json:"to"`
	Value float64 `json:"value"`
	Days  []int   `json:"days,omitempty"`
}

type CronEntry struct {
	ID    string `json:"id"`
	Cron  string `json:"cron"`
	Value int    `json:"value"`
}

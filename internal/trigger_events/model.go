package trigger_events

import "time"

type Reading struct {
	Peripheral string  `json:"peripheral"`
	Value      float64 `json:"value"`
}

type TriggerEvent struct {
	ID             string
	TriggerEventID string
	TriggerID      string
	FiredAt        time.Time
	ReceivedAt     time.Time
	Readings       []Reading
}

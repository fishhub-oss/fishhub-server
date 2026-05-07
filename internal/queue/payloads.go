package queue

import "time"

type Reading struct {
	Peripheral string  `json:"peripheral"`
	Value      float64 `json:"value"`
}

type AlertJobPayload struct {
	ActionID  string    `json:"action_id"`
	EventID   string    `json:"event_id"`
	TriggerID string    `json:"trigger_id"`
	FiredAt   time.Time `json:"fired_at"`
	Readings  []Reading `json:"readings"`
}

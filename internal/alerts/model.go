package alerts

import "time"

type Alert struct {
	ID        string
	UserID    string
	DeviceID  string
	TriggerID string
	EventID   string
	Severity  string
	Message   string
	Context   map[string]any
	CreatedAt time.Time
}

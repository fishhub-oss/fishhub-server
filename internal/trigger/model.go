package trigger

import (
	"encoding/json"
	"time"
)

type Trigger struct {
	ID                 string
	DeviceID           string
	Name               string
	Enabled            bool
	Condition          json.RawMessage
	TargetPeripheralID string
	Action             json.RawMessage
	CooldownSeconds    int
	CreatedAt          time.Time
}

type TriggerCreate struct {
	Name               string
	Condition          json.RawMessage
	TargetPeripheralID string
	Action             json.RawMessage
	CooldownSeconds    int
}

// TriggerUpdate holds fully-merged values for an update (all fields required).
type TriggerUpdate struct {
	Name            string
	Condition       json.RawMessage
	Action          json.RawMessage
	CooldownSeconds int
	Enabled         bool
}

// TriggerPatch holds the partial fields from a PATCH request (nil means no change).
type TriggerPatch struct {
	Name            *string
	Condition       json.RawMessage
	Action          json.RawMessage
	CooldownSeconds *int
	Enabled         *bool
}

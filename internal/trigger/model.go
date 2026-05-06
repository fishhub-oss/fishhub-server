package trigger

import (
	"encoding/json"
	"time"
)

type Action struct {
	ID     string
	Type   string
	Config json.RawMessage
}

type Trigger struct {
	ID              string
	DeviceID        string
	Name            string
	Enabled         bool
	Condition       json.RawMessage
	Actions         []Action
	CooldownSeconds int
	CreatedAt       time.Time
}

type TriggerCreate struct {
	Name            string
	Condition       json.RawMessage
	Actions         []Action
	CooldownSeconds int
}

// TriggerUpdate holds fully-merged values for an update (all fields required).
type TriggerUpdate struct {
	Name            string
	Condition       json.RawMessage
	Actions         []Action
	CooldownSeconds int
	Enabled         bool
}

// TriggerPatch holds the partial fields from a PATCH request (nil means no change).
// Actions nil = no change; empty slice is disallowed.
type TriggerPatch struct {
	Name            *string
	Condition       json.RawMessage
	Actions         []Action
	CooldownSeconds *int
	Enabled         *bool
}

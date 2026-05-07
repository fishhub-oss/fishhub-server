package trigger_events

import (
	"context"
	"encoding/json"
	"time"
)

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

type TriggerAction struct {
	ID     string
	Type   string
	Config json.RawMessage
}

// ActionGetter is a narrow interface for resolving trigger actions.
// Defined here to avoid cross-domain imports — main.go wires a bridge.
type ActionGetter interface {
	GetActions(ctx context.Context, triggerID string) ([]TriggerAction, error)
}

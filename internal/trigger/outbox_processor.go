package trigger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

// TriggerPushProcessor publishes trigger config to the firmware via retained MQTT.
type TriggerPushProcessor struct {
	publisher mqtt.Publisher
	logger    *slog.Logger
}

func NewTriggerPushProcessor(publisher mqtt.Publisher, logger *slog.Logger) *TriggerPushProcessor {
	return &TriggerPushProcessor{publisher: publisher, logger: logger}
}

func (p *TriggerPushProcessor) EventType() string { return eventTypeTriggerPush }

func (p *TriggerPushProcessor) Process(ctx context.Context, event outbox.Event) error {
	var payload triggerPushPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	topic := fmt.Sprintf("fishhub/%s/triggers/%s", payload.DeviceID, payload.ID)

	if payload.Op == "delete" {
		msg, err := json.Marshal(struct {
			Op string `json:"op"`
			ID string `json:"id"`
		}{Op: "delete", ID: payload.ID})
		if err != nil {
			return fmt.Errorf("marshal delete message: %w", err)
		}
		if err := p.publisher.PublishRetained(ctx, topic, msg); err != nil {
			p.logger.Error("trigger push: mqtt publish delete",
				"device_id", payload.DeviceID, "trigger_id", payload.ID, "error", err)
			return fmt.Errorf("mqtt publish delete: %w", err)
		}
		// Clear retained message (best effort — firmware acts on op:delete above).
		if err := p.publisher.PublishRetained(ctx, topic, []byte{}); err != nil {
			p.logger.Warn("trigger push: mqtt clear retained",
				"device_id", payload.DeviceID, "trigger_id", payload.ID, "error", err)
		}
		return nil
	}

	msg, err := json.Marshal(struct {
		Op        string          `json:"op"`
		ID        string          `json:"id"`
		Enabled   bool            `json:"enabled"`
		Condition json.RawMessage `json:"condition"`
		Actions   []actionPayload `json:"actions"`
		CooldownS int             `json:"cooldown_s"`
	}{
		Op:        "upsert",
		ID:        payload.ID,
		Enabled:   payload.Enabled,
		Condition: payload.Condition,
		Actions:   payload.Actions,
		CooldownS: payload.CooldownS,
	})
	if err != nil {
		return fmt.Errorf("marshal upsert message: %w", err)
	}

	if err := p.publisher.PublishRetained(ctx, topic, msg); err != nil {
		p.logger.Error("trigger push: mqtt publish upsert",
			"device_id", payload.DeviceID, "trigger_id", payload.ID, "error", err)
		return fmt.Errorf("mqtt publish upsert: %w", err)
	}
	return nil
}

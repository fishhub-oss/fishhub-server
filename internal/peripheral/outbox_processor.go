package peripheral

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

// PeripheralPushProcessor publishes peripheral config to the firmware via retained MQTT.
type PeripheralPushProcessor struct {
	publisher mqtt.Publisher
	logger    *slog.Logger
}

func NewPeripheralPushProcessor(publisher mqtt.Publisher, logger *slog.Logger) *PeripheralPushProcessor {
	return &PeripheralPushProcessor{publisher: publisher, logger: logger}
}

func (p *PeripheralPushProcessor) EventType() string { return eventTypePeripheralPush }

func (p *PeripheralPushProcessor) Process(ctx context.Context, event outbox.Event) error {
	var payload peripheralPushPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	msg, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal mqtt message: %w", err)
	}

	topic := fmt.Sprintf("fishhub/%s/peripherals/%s", payload.DeviceID, payload.Name)
	if err := p.publisher.PublishRetained(ctx, topic, msg); err != nil {
		p.logger.Error("peripheral push: mqtt publish", "device_id", payload.DeviceID, "name", payload.Name, "error", err)
		return fmt.Errorf("mqtt publish: %w", err)
	}
	return nil
}

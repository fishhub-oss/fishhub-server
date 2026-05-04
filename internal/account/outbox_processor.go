package account

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

const eventTypeDeviceConfigPush = "device.config_push"
const configPushClaimTimeoutSeconds = 30

type deviceConfigPushPayload struct {
	DeviceID string `json:"device_id"`
	Timezone string `json:"timezone"`
}

// ConfigPushProcessor publishes the device config (timezone) to the firmware
// via a retained MQTT message on fishhub/<device_id>/config.
type ConfigPushProcessor struct {
	publisher mqtt.Publisher
	logger    *slog.Logger
}

func NewConfigPushProcessor(publisher mqtt.Publisher, logger *slog.Logger) *ConfigPushProcessor {
	return &ConfigPushProcessor{publisher: publisher, logger: logger}
}

func (p *ConfigPushProcessor) EventType() string { return eventTypeDeviceConfigPush }

func (p *ConfigPushProcessor) Process(ctx context.Context, event outbox.Event) error {
	var payload deviceConfigPushPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	msg, err := json.Marshal(map[string]string{"timezone": payload.Timezone})
	if err != nil {
		return fmt.Errorf("marshal mqtt message: %w", err)
	}

	topic := fmt.Sprintf("fishhub/%s/config", payload.DeviceID)
	if err := p.publisher.PublishRetained(ctx, topic, msg); err != nil {
		p.logger.Error("config push: mqtt publish",
			"device_id", payload.DeviceID,
			"timezone", payload.Timezone,
			"error", err,
		)
		return fmt.Errorf("mqtt publish: %w", err)
	}
	return nil
}

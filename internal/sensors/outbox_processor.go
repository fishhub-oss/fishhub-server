package sensors

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

const EventTypeHiveMQProvision = "hivemq.provision_device"
const hiveMQProvisionClaimTimeoutSeconds = 30

type HiveMQProvisionPayload struct {
	DeviceID string `json:"device_id"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type HiveMQProvisionProcessor struct {
	hiveMQ hivemq.Client
	logger *slog.Logger
}

func NewHiveMQProvisionProcessor(hiveMQ hivemq.Client, logger *slog.Logger) *HiveMQProvisionProcessor {
	return &HiveMQProvisionProcessor{hiveMQ: hiveMQ, logger: logger}
}

func (p *HiveMQProvisionProcessor) EventType() string { return EventTypeHiveMQProvision }

func (p *HiveMQProvisionProcessor) Process(ctx context.Context, event outbox.Event) error {
	var payload HiveMQProvisionPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	err := p.hiveMQ.ProvisionDevice(ctx, payload.Username, payload.Password)
	if err != nil && isAlreadyExists(err) {
		p.logger.Info("hivemq provision: credential already exists, treating as success",
			"device_id", payload.DeviceID)
		return nil
	}
	return err
}

// isAlreadyExists reports whether the HiveMQ API error indicates the credential
// already exists (HTTP 409 Conflict).
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "409")
}

const EventTypePeripheralPush = "peripheral.push"
const peripheralPushClaimTimeoutSeconds = 30

type PeripheralPushPayload struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Op       string `json:"op"` // "create" or "delete"
	Kind     string `json:"kind,omitempty"`
	Pin      int    `json:"pin,omitempty"`
}

// PeripheralPushProcessor publishes peripheral config to the firmware via retained MQTT.
type PeripheralPushProcessor struct {
	publisher mqtt.Publisher
	logger    *slog.Logger
}

func NewPeripheralPushProcessor(publisher mqtt.Publisher, logger *slog.Logger) *PeripheralPushProcessor {
	return &PeripheralPushProcessor{publisher: publisher, logger: logger}
}

func (p *PeripheralPushProcessor) EventType() string { return EventTypePeripheralPush }

func (p *PeripheralPushProcessor) Process(ctx context.Context, event outbox.Event) error {
	var payload PeripheralPushPayload
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

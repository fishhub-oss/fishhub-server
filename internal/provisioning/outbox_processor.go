package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/fishhub-oss/fishhub-server/internal/mqttbroker"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

const EventTypeMQTTProvision = "mqtt.provision_device"
const mqttProvisionClaimTimeoutSeconds = 30

// eventTypeDeviceConfigPush and its payload are defined here for use by
// ActivationService. The account package defines its own copy of these for
// the PATCH /api/me trigger — both use the same event type string.
const eventTypeDeviceConfigPush = "device.config_push"
const configPushClaimTimeoutSeconds = 30

type deviceConfigPushPayload struct {
	DeviceID string `json:"device_id"`
	Timezone string `json:"timezone"`
}

type MQTTProvisionPayload struct {
	DeviceID string `json:"device_id"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type MQTTProvisionProcessor struct {
	provisioner mqttbroker.Provisioner
	logger      *slog.Logger
}

func NewMQTTProvisionProcessor(provisioner mqttbroker.Provisioner, logger *slog.Logger) *MQTTProvisionProcessor {
	return &MQTTProvisionProcessor{provisioner: provisioner, logger: logger}
}

func (p *MQTTProvisionProcessor) EventType() string { return EventTypeMQTTProvision }

func (p *MQTTProvisionProcessor) Process(ctx context.Context, event outbox.Event) error {
	var payload MQTTProvisionPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	err := p.provisioner.ProvisionDevice(ctx, payload.Username, payload.Password)
	if err != nil && isAlreadyExists(err) {
		p.logger.Info("mqtt provision: credential already exists, treating as success",
			"device_id", payload.DeviceID)
		return nil
	}
	return err
}

// isAlreadyExists reports whether the broker API error indicates the credential
// already exists (HTTP 409 Conflict).
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "409")
}

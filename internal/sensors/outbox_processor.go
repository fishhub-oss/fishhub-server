package sensors

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

const EventTypeHiveMQProvision = "hivemq.provision_device"

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

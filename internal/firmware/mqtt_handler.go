package firmware

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
)

// StatusMQTTHandler handles retained fishhub/+/status messages published by devices.
type StatusMQTTHandler struct {
	service *Service
	logger  *slog.Logger
}

func NewStatusMQTTHandler(service *Service, logger *slog.Logger) *StatusMQTTHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &StatusMQTTHandler{service: service, logger: logger}
}

// Handle is the mqtt.MessageHandler callback. Topic: fishhub/{device_id}/status.
func (h *StatusMQTTHandler) Handle(ctx context.Context, topic string, payload []byte) {
	if len(payload) == 0 {
		return
	}

	deviceID, ok := deviceIDFromTopic(topic)
	if !ok {
		h.logger.Warn("firmware status: unexpected topic", "topic", topic)
		return
	}

	var msg struct {
		FirmwareVersion  string `json:"firmware_version"`
		LastUpdateResult string `json:"last_update_result"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.logger.Warn("firmware status: invalid payload", "device_id", deviceID, "error", err)
		return
	}
	if msg.FirmwareVersion == "" {
		h.logger.Warn("firmware status: missing firmware_version", "device_id", deviceID)
		return
	}

	if err := h.service.IngestStatus(ctx, deviceID, msg.FirmwareVersion, msg.LastUpdateResult); err != nil {
		h.logger.Error("firmware status: ingest failed", "device_id", deviceID, "error", err)
	}
}

// deviceIDFromTopic extracts the device ID from fishhub/{device_id}/status.
func deviceIDFromTopic(topic string) (string, bool) {
	parts := strings.Split(topic, "/")
	if len(parts) != 3 || parts[0] != "fishhub" || parts[2] != "status" {
		return "", false
	}
	if parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

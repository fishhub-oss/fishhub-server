package trigger_events

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

type MQTTHandler struct {
	store  Store
	logger *slog.Logger
}

func NewMQTTHandler(store Store, logger *slog.Logger) *MQTTHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &MQTTHandler{store: store, logger: logger}
}

type mqttPayload struct {
	TriggerEventID string    `json:"trigger_event_id"`
	TriggerID      string    `json:"trigger_id"`
	DeviceID       string    `json:"device_id"`
	FiredAt        string    `json:"fired_at"`
	Readings       []Reading `json:"readings"`
}

// Handle is the mqtt.MessageHandler callback. Topic shape: fishhub/{device_id}/trigger_events.
func (h *MQTTHandler) Handle(ctx context.Context, topic string, payload []byte) {
	deviceID, ok := deviceIDFromTopic(topic)
	if !ok {
		h.logger.Warn("mqtt trigger_events: unexpected topic shape", "topic", topic)
		return
	}

	var p mqttPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.logger.Warn("mqtt trigger_events: invalid payload", "device_id", deviceID, "error", err)
		return
	}
	if p.TriggerEventID == "" || p.TriggerID == "" || p.FiredAt == "" {
		h.logger.Warn("mqtt trigger_events: missing required fields", "device_id", deviceID)
		return
	}

	ok, err := h.store.TriggerBelongsToDevice(ctx, p.TriggerID, deviceID)
	if err != nil {
		h.logger.Error("mqtt trigger_events: ownership check", "device_id", deviceID, "trigger_id", p.TriggerID, "error", err)
		return
	}
	if !ok {
		h.logger.Warn("mqtt trigger_events: trigger does not belong to device", "device_id", deviceID, "trigger_id", p.TriggerID)
		return
	}

	firedAt, err := time.Parse(time.RFC3339, p.FiredAt)
	if err != nil {
		h.logger.Warn("mqtt trigger_events: invalid fired_at", "device_id", deviceID, "fired_at", p.FiredAt, "error", err)
		return
	}

	if err := h.store.Ingest(ctx, TriggerEvent{
		TriggerEventID: p.TriggerEventID,
		TriggerID:      p.TriggerID,
		FiredAt:        firedAt,
		Readings:       p.Readings,
	}); err != nil {
		h.logger.Error("mqtt trigger_events: ingest", "device_id", deviceID, "trigger_id", p.TriggerID, "error", err)
	}
}

func deviceIDFromTopic(topic string) (string, bool) {
	parts := strings.Split(topic, "/")
	if len(parts) != 3 || parts[0] != "fishhub" || parts[2] != "trigger_events" {
		return "", false
	}
	if parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

package trigger_events

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/queue"
	"github.com/google/uuid"
)

// deviceSideActionTypes are executed on-device and must never be enqueued server-side.
var deviceSideActionTypes = map[string]bool{
	"peripheral_action": true,
}

type MQTTHandler struct {
	store   Store
	actions ActionGetter
	queue   queue.Queue
	logger  *slog.Logger
}

func NewMQTTHandler(store Store, actions ActionGetter, q queue.Queue, logger *slog.Logger) *MQTTHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &MQTTHandler{store: store, actions: actions, queue: q, logger: logger}
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

	event := TriggerEvent{
		TriggerEventID: p.TriggerEventID,
		TriggerID:      p.TriggerID,
		FiredAt:        firedAt,
		Readings:       p.Readings,
	}

	ingested, err := h.store.Ingest(ctx, event)
	if err != nil {
		h.logger.Error("mqtt trigger_events: ingest", "device_id", deviceID, "trigger_id", p.TriggerID, "error", err)
		return
	}

	h.enqueueActions(ctx, ingested)
}

func (h *MQTTHandler) enqueueActions(ctx context.Context, event TriggerEvent) {
	actions, err := h.actions.GetActions(ctx, event.TriggerID)
	if err != nil {
		h.logger.Warn("mqtt trigger_events: get actions for enqueue", "trigger_id", event.TriggerID, "error", err)
		return
	}

	for _, action := range actions {
		if deviceSideActionTypes[action.Type] {
			continue
		}

		jobPayload, err := json.Marshal(queue.AlertJobPayload{
			ActionID:  action.ID,
			EventID:   event.ID,
			TriggerID: event.TriggerID,
			FiredAt:   event.FiredAt,
			Readings:  readingsToQueueReadings(event.Readings),
		})
		if err != nil {
			h.logger.Warn("mqtt trigger_events: marshal job payload", "action_id", action.ID, "error", err)
			continue
		}

		if err := h.queue.Enqueue(ctx, "trigger-actions", queue.Job{
			ID:      uuid.New().String(),
			Type:    action.Type,
			Payload: jobPayload,
		}); err != nil {
			h.logger.Warn("mqtt trigger_events: enqueue job", "action_id", action.ID, "action_type", action.Type, "error", err)
		}
	}
}

func readingsToQueueReadings(r []Reading) []queue.Reading {
	out := make([]queue.Reading, len(r))
	for i, v := range r {
		out[i] = queue.Reading{Peripheral: v.Peripheral, Value: v.Value}
	}
	return out
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

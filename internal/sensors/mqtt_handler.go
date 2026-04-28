package sensors

import (
	"context"
	"errors"
	"log/slog"
	"strings"
)

// ReadingsMQTTHandler handles incoming readings published by devices to
// fishhub/+/readings. It reuses the same pipeline as the HTTP handler.
type ReadingsMQTTHandler struct {
	store   DeviceStore
	service *ReadingsService
	logger  *slog.Logger
}

func NewReadingsMQTTHandler(store DeviceStore, service *ReadingsService, logger *slog.Logger) *ReadingsMQTTHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReadingsMQTTHandler{store: store, service: service, logger: logger}
}

// Handle is the mqtt.MessageHandler callback. Topic shape: fishhub/{device_id}/readings.
func (h *ReadingsMQTTHandler) Handle(ctx context.Context, topic string, payload []byte) {
	deviceID, ok := deviceIDFromTopic(topic)
	if !ok {
		h.logger.Warn("mqtt readings: unexpected topic shape", "topic", topic)
		return
	}

	device, err := h.store.FindByID(ctx, deviceID)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			h.logger.Warn("mqtt readings: device not found", "device_id", deviceID)
		} else {
			h.logger.Error("mqtt readings: store lookup", "device_id", deviceID, "error", err)
		}
		return
	}

	if err := h.service.Write(ctx, DeviceInfo{DeviceID: device.ID, UserID: device.UserID}, payload); err != nil {
		h.logger.Error("mqtt readings: write failed", "device_id", deviceID, "error", err)
	}
}

// deviceIDFromTopic extracts the device ID from fishhub/{device_id}/readings.
func deviceIDFromTopic(topic string) (string, bool) {
	parts := strings.Split(topic, "/")
	if len(parts) != 3 || parts[0] != "fishhub" || parts[2] != "readings" {
		return "", false
	}
	id := parts[1]
	if id == "" {
		return "", false
	}
	return id, true
}

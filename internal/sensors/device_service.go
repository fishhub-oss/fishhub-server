package sensors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
)

// DeviceService orchestrates multi-step device operations.
type DeviceService struct {
	store     DeviceStore
	hiveMQ    hivemq.Client
	publisher CommandPublisher
	logger    *slog.Logger
}

func NewDeviceService(store DeviceStore, hiveMQ hivemq.Client, publisher CommandPublisher, logger *slog.Logger) *DeviceService {
	return &DeviceService{store: store, hiveMQ: hiveMQ, publisher: publisher, logger: logger}
}

// Delete soft-deletes the device and revokes its MQTT credentials.
// Returns ErrDeviceNotFound unwrapped if the device does not exist or is not
// owned by userID.
func (s *DeviceService) Delete(ctx context.Context, deviceID, userID string) error {
	mqttUsername, err := s.store.DeleteDevice(ctx, deviceID, userID)
	if err != nil {
		return err
	}
	if mqttUsername != "" {
		if err := s.hiveMQ.DeleteDevice(ctx, mqttUsername); err != nil {
			s.logger.Warn("hivemq delete device", "device_id", deviceID, "error", err)
		}
	}
	return nil
}

// List returns all devices belonging to userID, optionally filtered by status.
func (s *DeviceService) List(ctx context.Context, userID, status string) ([]Device, error) {
	return s.store.ListByUserID(ctx, userID, status)
}

// Patch updates the device name and returns the updated device.
// Returns ErrDeviceNotFound unwrapped if the device does not exist or is not
// owned by userID.
func (s *DeviceService) Patch(ctx context.Context, deviceID, userID, name string) (Device, error) {
	return s.store.PatchDevice(ctx, deviceID, userID, name)
}

// SendCommand verifies ownership and publishes the raw command payload to the
// device's MQTT topic. Returns ErrDeviceNotFound unwrapped if the device does
// not exist or is not owned by userID.
func (s *DeviceService) SendCommand(ctx context.Context, deviceID, userID, peripheralName string, body []byte) error {
	if _, err := s.store.FindByIDAndUserID(ctx, deviceID, userID); err != nil {
		return err
	}

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(io.NopCloser(bytes.NewReader(body))).Decode(&req); err != nil ||
		(req.Action != "set" && req.Action != "schedule") {
		return ErrInvalidCommand
	}

	topic := fmt.Sprintf("fishhub/%s/commands/%s", deviceID, peripheralName)
	if err := s.publisher.Publish(ctx, topic, body); err != nil {
		return fmt.Errorf("mqtt publish: %w", err)
	}
	return nil
}

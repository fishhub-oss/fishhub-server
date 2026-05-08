package device

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/mqttbroker"
)

// Service orchestrates multi-step device operations.
type Service struct {
	store       Store
	provisioner mqttbroker.Provisioner
	publisher   mqtt.Publisher
	logger      *slog.Logger
}

func NewService(store Store, provisioner mqttbroker.Provisioner, publisher mqtt.Publisher, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, provisioner: provisioner, publisher: publisher, logger: logger}
}

// Delete soft-deletes the device and revokes its MQTT credentials.
// Returns ErrNotFound unwrapped if the device does not exist or is not owned by userID.
func (s *Service) Delete(ctx context.Context, deviceID, userID string) error {
	mqttUsername, err := s.store.DeleteDevice(ctx, deviceID, userID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Error("delete device", "device_id", deviceID, "error", err)
		}
		return err
	}
	if mqttUsername != "" {
		if err := s.provisioner.DeleteDevice(ctx, mqttUsername); err != nil {
			s.logger.Warn("mqtt broker delete device", "device_id", deviceID, "error", err)
		}
	}
	return nil
}

// List returns all devices belonging to userID.
func (s *Service) List(ctx context.Context, userID string) ([]Device, error) {
	devices, err := s.store.ListByUserID(ctx, userID)
	if err != nil {
		s.logger.Error("list devices", "user_id", userID, "error", err)
	}
	return devices, err
}

// Patch updates the device name and returns the updated device.
// Returns ErrNotFound unwrapped if the device does not exist or is not owned by userID.
func (s *Service) Patch(ctx context.Context, deviceID, userID, name string) (Device, error) {
	d, err := s.store.PatchDevice(ctx, deviceID, userID, name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		s.logger.Error("patch device", "device_id", deviceID, "error", err)
	}
	return d, err
}

// SendCommand verifies ownership and publishes the raw command payload to the
// device's MQTT topic. Returns ErrNotFound unwrapped if the device does not
// exist or is not owned by userID.
func (s *Service) SendCommand(ctx context.Context, deviceID, userID, peripheralName string, body []byte) error {
	if _, err := s.store.FindByIDAndUserID(ctx, deviceID, userID); err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Error("send command: find device", "device_id", deviceID, "error", err)
		}
		return err
	}

	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(io.NopCloser(bytes.NewReader(body))).Decode(&req); err != nil ||
		(req.Command != "set" && req.Command != "schedule") {
		return ErrInvalidCommand
	}

	topic := fmt.Sprintf("fishhub/%s/commands/%s", deviceID, peripheralName)
	if err := s.publisher.Publish(ctx, topic, body); err != nil {
		s.logger.Error("send command: mqtt publish", "device_id", deviceID, "error", err)
		return fmt.Errorf("mqtt publish: %w", err)
	}
	return nil
}

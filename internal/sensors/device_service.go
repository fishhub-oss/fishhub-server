package sensors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
)

// DeviceService orchestrates multi-step device operations.
type DeviceService struct {
	Store     DeviceStore
	HiveMQ    hivemq.Client
	Publisher CommandPublisher
}

// Delete soft-deletes the device and revokes its MQTT credentials.
// Returns ErrDeviceNotFound unwrapped if the device does not exist or is not
// owned by userID.
func (s *DeviceService) Delete(ctx context.Context, deviceID, userID string) error {
	mqttUsername, err := s.Store.DeleteDevice(ctx, deviceID, userID)
	if err != nil {
		return err
	}
	if mqttUsername != "" {
		if err := s.HiveMQ.DeleteDevice(ctx, mqttUsername); err != nil {
			log.Printf("hivemq delete device error (device_id=%s): %v", deviceID, err)
		}
	}
	return nil
}

// SendCommand verifies ownership and publishes the raw command payload to the
// device's MQTT topic. Returns ErrDeviceNotFound unwrapped if the device does
// not exist or is not owned by userID.
func (s *DeviceService) SendCommand(ctx context.Context, deviceID, userID, peripheralName string, body []byte) error {
	if _, err := s.Store.FindByIDAndUserID(ctx, deviceID, userID); err != nil {
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
	if err := s.Publisher.Publish(ctx, topic, body); err != nil {
		return fmt.Errorf("mqtt publish: %w", err)
	}
	return nil
}

package sensors

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
)

// ActivationResult holds everything the device needs after successful activation.
type ActivationResult struct {
	Token        string
	DeviceID     string
	MQTTUsername string
	MQTTPassword string
	MQTTHost     string
	MQTTPort     int
}

// ActivationService orchestrates device activation: claim code → provision MQTT
// credentials → store in DB → sign JWT.
type ActivationService struct {
	store    ProvisioningStore
	hiveMQ   hivemq.Client
	signer   devicejwt.Signer
	mqttHost string
	mqttPort int
	logger   *slog.Logger
}

func NewActivationService(store ProvisioningStore, hiveMQ hivemq.Client, signer devicejwt.Signer, mqttHost string, mqttPort int, logger *slog.Logger) *ActivationService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ActivationService{store: store, hiveMQ: hiveMQ, signer: signer, mqttHost: mqttHost, mqttPort: mqttPort, logger: logger}
}

// Activate claims the provisioning code and completes device activation.
// Sentinel errors ErrCodeNotFound and ErrCodeAlreadyUsed are returned unwrapped
// so callers can map them to HTTP status codes.
func (s *ActivationService) Activate(ctx context.Context, code string) (ActivationResult, error) {
	deviceID, userID, err := s.store.ClaimCode(ctx, code)
	if err != nil {
		if err != ErrCodeNotFound && err != ErrCodeAlreadyUsed {
			s.logger.Error("activate: claim code", "error", err)
		}
		return ActivationResult{}, err
	}

	mqttUsername := deviceID
	mqttPasswordBytes := make([]byte, 32)
	if _, err := rand.Read(mqttPasswordBytes); err != nil {
		s.logger.Error("activate: generate mqtt password", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("generate mqtt password: %w", err)
	}
	mqttPassword := hex.EncodeToString(mqttPasswordBytes)

	if err := s.hiveMQ.ProvisionDevice(ctx, mqttUsername, mqttPassword); err != nil {
		s.logger.Error("activate: hivemq provision", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("hivemq provision: %w", err)
	}

	if err := s.store.Activate(ctx, deviceID, mqttUsername, mqttPassword); err != nil {
		s.logger.Error("activate: store", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("activate device: %w", err)
	}

	jwtToken, err := s.signer.Sign(deviceID, userID)
	if err != nil {
		s.logger.Error("activate: sign device jwt", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("sign device jwt: %w", err)
	}

	return ActivationResult{
		Token:        jwtToken,
		DeviceID:     deviceID,
		MQTTUsername: mqttUsername,
		MQTTPassword: mqttPassword,
		MQTTHost:     s.mqttHost,
		MQTTPort:     s.mqttPort,
	}, nil
}

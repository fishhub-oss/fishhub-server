package sensors

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

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
	Store    ProvisioningStore
	HiveMQ   hivemq.Client
	Signer   devicejwt.Signer
	MQTTHost string
	MQTTPort int
}

// Activate claims the provisioning code and completes device activation.
// Sentinel errors ErrCodeNotFound and ErrCodeAlreadyUsed are returned unwrapped
// so callers can map them to HTTP status codes.
func (s *ActivationService) Activate(ctx context.Context, code string) (ActivationResult, error) {
	deviceID, userID, err := s.Store.ClaimCode(ctx, code)
	if err != nil {
		return ActivationResult{}, err
	}

	mqttUsername := deviceID
	mqttPasswordBytes := make([]byte, 32)
	if _, err := rand.Read(mqttPasswordBytes); err != nil {
		return ActivationResult{}, fmt.Errorf("generate mqtt password: %w", err)
	}
	mqttPassword := hex.EncodeToString(mqttPasswordBytes)

	if err := s.HiveMQ.ProvisionDevice(ctx, mqttUsername, mqttPassword); err != nil {
		return ActivationResult{}, fmt.Errorf("hivemq provision: %w", err)
	}

	if err := s.Store.Activate(ctx, deviceID, mqttUsername, mqttPassword); err != nil {
		return ActivationResult{}, fmt.Errorf("activate device: %w", err)
	}

	jwtToken, err := s.Signer.Sign(deviceID, userID)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("sign device jwt: %w", err)
	}

	return ActivationResult{
		Token:        jwtToken,
		DeviceID:     deviceID,
		MQTTUsername: mqttUsername,
		MQTTPassword: mqttPassword,
		MQTTHost:     s.MQTTHost,
		MQTTPort:     s.MQTTPort,
	}, nil
}

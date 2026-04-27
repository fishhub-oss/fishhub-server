package sensors

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

// ActivationResult holds what the device receives immediately after activation.
// MQTT credentials are not included — the device polls GET /devices/{id}/status
// until they are ready.
type ActivationResult struct {
	Token    string
	DeviceID string
}

// ActivationService orchestrates device activation: claim code → store credentials
// + enqueue HiveMQ provisioning atomically → sign JWT.
type ActivationService struct {
	db          *sql.DB
	store       ProvisioningStore
	outboxStore outbox.Store
	signer      devicejwt.Signer
	logger      *slog.Logger
}

func NewActivationService(
	db *sql.DB,
	store ProvisioningStore,
	outboxStore outbox.Store,
	signer devicejwt.Signer,
	logger *slog.Logger,
) *ActivationService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ActivationService{
		db:          db,
		store:       store,
		outboxStore: outboxStore,
		signer:      signer,
		logger:      logger,
	}
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger.Error("activate: begin tx", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if err := s.store.Activate(ctx, tx, deviceID, mqttUsername, mqttPassword); err != nil {
		s.logger.Error("activate: store credentials", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("activate device: %w", err)
	}

	if err := s.outboxStore.Insert(ctx, tx, EventTypeHiveMQProvision, HiveMQProvisionPayload{
		DeviceID: deviceID,
		Username: mqttUsername,
		Password: mqttPassword,
	}, hiveMQProvisionClaimTimeoutSeconds); err != nil {
		s.logger.Error("activate: enqueue hivemq provision", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("enqueue hivemq provision: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("activate: commit tx", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("commit tx: %w", err)
	}

	jwtToken, err := s.signer.Sign(deviceID, userID)
	if err != nil {
		s.logger.Error("activate: sign device jwt", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("sign device jwt: %w", err)
	}

	return ActivationResult{
		Token:    jwtToken,
		DeviceID: deviceID,
	}, nil
}

package provisioning

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

// TimezoneReader looks up a user's timezone. Implemented by a bridge in main.go.
type TimezoneReader interface {
	GetTimezone(ctx context.Context, userID string) (string, error)
}

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
	db             *sql.DB
	store          Store
	outboxStore    outbox.Store
	signer         devicejwt.Signer
	timezoneReader TimezoneReader
	logger         *slog.Logger
}

func NewActivationService(
	db *sql.DB,
	store Store,
	outboxStore outbox.Store,
	signer devicejwt.Signer,
	timezoneReader TimezoneReader,
	logger *slog.Logger,
) *ActivationService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ActivationService{
		db:             db,
		store:          store,
		outboxStore:    outboxStore,
		signer:         signer,
		timezoneReader: timezoneReader,
		logger:         logger,
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

	// Look up the user's timezone before opening the transaction.
	// Defaults to "UTC" if the account is not yet created or the lookup fails.
	timezone := "UTC"
	if tz, err := s.timezoneReader.GetTimezone(ctx, userID); err == nil {
		timezone = tz
	} else {
		s.logger.Warn("activate: timezone lookup failed, defaulting to UTC",
			"user_id", userID, "error", err)
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

	if err := s.outboxStore.Insert(ctx, tx, EventTypeMQTTProvision, MQTTProvisionPayload{
		DeviceID: deviceID,
		Username: mqttUsername,
		Password: mqttPassword,
	}, mqttProvisionClaimTimeoutSeconds); err != nil {
		s.logger.Error("activate: enqueue mqtt provision", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("enqueue mqtt provision: %w", err)
	}

	if err := s.outboxStore.Insert(ctx, tx, eventTypeDeviceConfigPush, deviceConfigPushPayload{
		DeviceID: deviceID,
		Timezone: timezone,
	}, configPushClaimTimeoutSeconds); err != nil {
		s.logger.Error("activate: enqueue config push", "device_id", deviceID, "error", err)
		return ActivationResult{}, fmt.Errorf("enqueue config push: %w", err)
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

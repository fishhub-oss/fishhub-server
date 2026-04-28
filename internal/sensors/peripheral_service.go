package sensors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

// PeripheralService orchestrates peripheral registration, listing, schedule updates, and deletion.
type PeripheralService struct {
	db        *sql.DB
	store     PeripheralStore
	outbox    outbox.Store
	publisher mqtt.Publisher
	logger    *slog.Logger
}

func NewPeripheralService(
	db *sql.DB,
	store PeripheralStore,
	outboxStore outbox.Store,
	publisher mqtt.Publisher,
	logger *slog.Logger,
) *PeripheralService {
	if logger == nil {
		logger = slog.Default()
	}
	return &PeripheralService{
		db:        db,
		store:     store,
		outbox:    outboxStore,
		publisher: publisher,
		logger:    logger,
	}
}

// Register creates a new peripheral and enqueues a peripheral.push outbox event atomically.
func (s *PeripheralService) Register(ctx context.Context, deviceID, userID, name, kind string, pin int) (Peripheral, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Peripheral{}, fmt.Errorf("register peripheral: begin tx: %w", err)
	}
	defer tx.Rollback()

	p, err := s.store.CreatePeripheral(ctx, tx, deviceID, userID, name, kind, pin)
	if err != nil {
		if !errors.Is(err, ErrDeviceNotFound) && !errors.Is(err, ErrPeripheralAlreadyExists) {
			s.logger.Error("register peripheral: create", "device_id", deviceID, "name", name, "error", err)
		}
		return Peripheral{}, err
	}

	if err := s.outbox.Insert(ctx, tx, EventTypePeripheralPush, PeripheralPushPayload{
		DeviceID: deviceID,
		Name:     name,
		Op:       "create",
		Kind:     kind,
		Pin:      pin,
	}, peripheralPushClaimTimeoutSeconds); err != nil {
		s.logger.Error("register peripheral: enqueue push", "device_id", deviceID, "name", name, "error", err)
		return Peripheral{}, fmt.Errorf("register peripheral: enqueue push: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("register peripheral: commit", "device_id", deviceID, "name", name, "error", err)
		return Peripheral{}, fmt.Errorf("register peripheral: commit: %w", err)
	}

	return p, nil
}

// List returns active peripherals for the device.
func (s *PeripheralService) List(ctx context.Context, deviceID, userID string) ([]Peripheral, error) {
	peripherals, err := s.store.ListPeripherals(ctx, deviceID, userID)
	if err != nil && !errors.Is(err, ErrDeviceNotFound) {
		s.logger.Error("list peripherals", "device_id", deviceID, "error", err)
	}
	return peripherals, err
}

// SetSchedule persists the schedule to DB and publishes it synchronously via MQTT.
func (s *PeripheralService) SetSchedule(ctx context.Context, deviceID, userID, name string, schedule []ScheduleWindow) (Peripheral, error) {
	p, err := s.store.SetPeripheralSchedule(ctx, deviceID, userID, name, schedule)
	if err != nil {
		if !errors.Is(err, ErrPeripheralNotFound) {
			s.logger.Error("set peripheral schedule: store", "device_id", deviceID, "name", name, "error", err)
		}
		return Peripheral{}, err
	}

	msg, err := json.Marshal(map[string]any{
		"action":  "schedule",
		"windows": schedule,
	})
	if err != nil {
		s.logger.Error("set peripheral schedule: marshal mqtt payload", "device_id", deviceID, "name", name, "error", err)
		return p, nil
	}

	topic := fmt.Sprintf("fishhub/%s/commands/%s", deviceID, name)
	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		s.logger.Warn("set peripheral schedule: mqtt publish failed", "device_id", deviceID, "name", name, "error", err)
	}

	return p, nil
}

// Delete soft-deletes the peripheral and enqueues a peripheral.push delete event atomically.
func (s *PeripheralService) Delete(ctx context.Context, deviceID, userID, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete peripheral: begin tx: %w", err)
	}
	defer tx.Rollback()

	if err := s.store.DeletePeripheral(ctx, tx, deviceID, userID, name); err != nil {
		if !errors.Is(err, ErrPeripheralNotFound) {
			s.logger.Error("delete peripheral: store", "device_id", deviceID, "name", name, "error", err)
		}
		return err
	}

	if err := s.outbox.Insert(ctx, tx, EventTypePeripheralPush, PeripheralPushPayload{
		DeviceID: deviceID,
		Name:     name,
		Op:       "delete",
	}, peripheralPushClaimTimeoutSeconds); err != nil {
		s.logger.Error("delete peripheral: enqueue push", "device_id", deviceID, "name", name, "error", err)
		return fmt.Errorf("delete peripheral: enqueue push: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("delete peripheral: commit", "device_id", deviceID, "name", name, "error", err)
		return fmt.Errorf("delete peripheral: commit: %w", err)
	}

	return nil
}

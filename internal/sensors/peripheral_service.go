package sensors

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/google/uuid"
)

// PeripheralService orchestrates peripheral registration, listing, schedule updates, and deletion.
type PeripheralService struct {
	db        *sql.DB
	store     PeripheralStore
	outbox    outbox.Store
	querier   ReadingQuerier
	publisher mqtt.Publisher
	logger    *slog.Logger
}

func NewPeripheralService(
	db *sql.DB,
	store PeripheralStore,
	outboxStore outbox.Store,
	querier ReadingQuerier,
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
		querier:   querier,
		publisher: publisher,
		logger:    logger,
	}
}

// Register creates a new peripheral and enqueues a peripheral.push outbox event atomically.
func (s *PeripheralService) Register(ctx context.Context, deviceID, userID, name, kind, category string, pin int) (Peripheral, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Peripheral{}, fmt.Errorf("register peripheral: begin tx: %w", err)
	}
	defer tx.Rollback()

	p, err := s.store.CreatePeripheral(ctx, tx, deviceID, userID, name, kind, category, pin)
	if err != nil {
		if !errors.Is(err, ErrDeviceNotFound) && !errors.Is(err, ErrPeripheralAlreadyExists) {
			s.logger.Error("register peripheral: create", "device_id", deviceID, "name", name, "error", err)
		}
		return Peripheral{}, err
	}

	if err := s.outbox.Insert(ctx, tx, EventTypePeripheralPush, PeripheralPushPayload{
		DeviceID: deviceID,
		Name:     fmt.Sprintf("%s-%d", kind, pin),
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

// List returns active peripherals for the device, each populated with its last known reading.
// Returns an empty slice if the device does not exist or is not owned by userID.
func (s *PeripheralService) List(ctx context.Context, deviceID, userID string) ([]Peripheral, error) {
	peripherals, err := s.store.ListPeripherals(ctx, deviceID, userID)
	if err != nil {
		s.logger.Error("list peripherals", "device_id", deviceID, "error", err)
		return peripherals, err
	}

	if s.querier != nil && len(peripherals) > 0 {
		last, err := s.querier.QueryLastReadings(ctx, deviceID)
		if err != nil {
			s.logger.Error("list peripherals: query last readings", "device_id", deviceID, "error", err)
			// Non-fatal: return peripherals without last_reading rather than failing the request.
		} else if last != nil {
			for i := range peripherals {
				ns := fmt.Sprintf("%s-%d", peripherals[i].Kind, peripherals[i].Pin)
				filtered := filterByNamespace(last, ns)
				if filtered != nil {
					peripherals[i].LastReading = filtered
				}
			}
		}
	}

	return peripherals, nil
}

// filterByNamespace returns a ReadingPoint containing only the values whose keys
// start with the given namespace prefix (e.g. "ds18b20-4"), or nil if none match.
func filterByNamespace(p *ReadingPoint, ns string) *ReadingPoint {
	prefix := ns + "/"
	filtered := &ReadingPoint{Timestamp: p.Timestamp, Values: make(map[string]any)}
	for k, v := range p.Values {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			filtered.Values[k] = v
		}
	}
	if len(filtered.Values) == 0 {
		return nil
	}
	return filtered
}

// SetSchedule persists the schedule to DB and publishes it synchronously via MQTT.
func (s *PeripheralService) SetSchedule(ctx context.Context, deviceID, userID, peripheralID string, schedule []ScheduleWindow) (Peripheral, error) {
	p, err := s.store.SetPeripheralSchedule(ctx, deviceID, userID, peripheralID, schedule)
	if err != nil {
		if !errors.Is(err, ErrPeripheralNotFound) {
			s.logger.Error("set peripheral schedule: store", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		}
		return Peripheral{}, err
	}

	msg, err := json.Marshal(map[string]any{
		"id":      uuid.NewString(),
		"action":  "schedule",
		"windows": schedule,
	})
	if err != nil {
		s.logger.Error("set peripheral schedule: marshal mqtt payload", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		return p, nil
	}

	topic := fmt.Sprintf("fishhub/%s/commands/%s-%d", deviceID, p.Kind, p.Pin)
	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		s.logger.Warn("set peripheral schedule: mqtt publish failed", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
	}

	return p, nil
}

// SetControlMode updates control_mode for an actuator peripheral.
// Publishes the set_mode MQTT command before committing — rolls back on publish failure.
func (s *PeripheralService) SetControlMode(ctx context.Context, deviceID, userID, peripheralID, mode string) (Peripheral, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Peripheral{}, fmt.Errorf("set control mode: begin tx: %w", err)
	}
	defer tx.Rollback()

	p, err := s.store.SetControlMode(ctx, tx, deviceID, userID, peripheralID, mode)
	if err != nil {
		if !errors.Is(err, ErrPeripheralNotFound) && !errors.Is(err, ErrNotAnActuator) {
			s.logger.Error("set control mode: store", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		}
		return Peripheral{}, err
	}

	msg, err := json.Marshal(map[string]any{
		"id":     uuid.NewString(),
		"action": "set_mode",
		"mode":   mode,
	})
	if err != nil {
		return Peripheral{}, fmt.Errorf("set control mode: marshal mqtt payload: %w", err)
	}

	topic := fmt.Sprintf("fishhub/%s/commands/%s-%d", deviceID, p.Kind, p.Pin)
	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		s.logger.Error("set control mode: mqtt publish failed", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		return Peripheral{}, fmt.Errorf("set control mode: mqtt publish: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("set control mode: commit", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		return Peripheral{}, fmt.Errorf("set control mode: commit: %w", err)
	}

	return p, nil
}

// SendCommand resolves the peripheral by ID to get its kind+pin, then publishes the raw
// command payload to the device's MQTT topic using the kind-pin address.
func (s *PeripheralService) SendCommand(ctx context.Context, deviceID, userID, peripheralID string, body []byte) error {
	p, err := s.store.GetPeripheral(ctx, deviceID, userID, peripheralID)
	if err != nil {
		if !errors.Is(err, ErrPeripheralNotFound) {
			s.logger.Error("send command: get peripheral", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		}
		return err
	}

	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil ||
		(req.Action != "set" && req.Action != "schedule") {
		return ErrInvalidCommand
	}

	topic := fmt.Sprintf("fishhub/%s/commands/%s-%d", deviceID, p.Kind, p.Pin)
	if err := s.publisher.Publish(ctx, topic, body); err != nil {
		s.logger.Error("send command: mqtt publish", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		return fmt.Errorf("mqtt publish: %w", err)
	}
	return nil
}

// Delete soft-deletes the peripheral and enqueues a peripheral.push delete event atomically.
func (s *PeripheralService) Delete(ctx context.Context, deviceID, userID, peripheralID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete peripheral: begin tx: %w", err)
	}
	defer tx.Rollback()

	deleted, err := s.store.DeletePeripheral(ctx, tx, deviceID, userID, peripheralID)
	if err != nil {
		if !errors.Is(err, ErrPeripheralNotFound) {
			s.logger.Error("delete peripheral: store", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		}
		return err
	}

	if err := s.outbox.Insert(ctx, tx, EventTypePeripheralPush, PeripheralPushPayload{
		DeviceID: deviceID,
		Name:     fmt.Sprintf("%s-%d", deleted.Kind, deleted.Pin),
		Op:       "delete",
	}, peripheralPushClaimTimeoutSeconds); err != nil {
		s.logger.Error("delete peripheral: enqueue push", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		return fmt.Errorf("delete peripheral: enqueue push: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("delete peripheral: commit", "device_id", deviceID, "peripheral_id", peripheralID, "error", err)
		return fmt.Errorf("delete peripheral: commit: %w", err)
	}

	return nil
}

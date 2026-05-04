package trigger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

const eventTypeTriggerPush = "trigger.push"
const triggerPushClaimTimeoutSeconds = 30

type triggerPushPayload struct {
	Op               string          `json:"op"`
	ID               string          `json:"id"`
	DeviceID         string          `json:"device_id"`
	Enabled          bool            `json:"enabled,omitempty"`
	Condition        json.RawMessage `json:"condition,omitempty"`
	TargetPeripheral string          `json:"target_peripheral,omitempty"`
	Action           json.RawMessage `json:"action,omitempty"`
	CooldownS        int             `json:"cooldown_s,omitempty"`
}

// Service orchestrates trigger CRUD and outbox event enqueuing.
type Service struct {
	db     *sql.DB
	store  Store
	outbox outbox.Store
	logger *slog.Logger
}

func NewService(db *sql.DB, store Store, outboxStore outbox.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		db:     db,
		store:  store,
		outbox: outboxStore,
		logger: logger,
	}
}

// Create inserts a trigger and enqueues a trigger.push outbox event atomically.
func (s *Service) Create(ctx context.Context, deviceID, userID string, p TriggerCreate) (Trigger, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Trigger{}, fmt.Errorf("create trigger: begin tx: %w", err)
	}
	defer tx.Rollback()

	t, kindPin, err := s.store.Create(ctx, tx, deviceID, userID, p)
	if err != nil {
		return Trigger{}, err
	}

	if err := s.outbox.Insert(ctx, tx, eventTypeTriggerPush, triggerPushPayload{
		Op:               "upsert",
		ID:               t.ID,
		DeviceID:         t.DeviceID,
		Enabled:          t.Enabled,
		Condition:        json.RawMessage(t.Condition),
		TargetPeripheral: kindPin,
		Action:           json.RawMessage(t.Action),
		CooldownS:        t.CooldownSeconds,
	}, triggerPushClaimTimeoutSeconds); err != nil {
		s.logger.Error("create trigger: enqueue push", "device_id", deviceID, "error", err)
		return Trigger{}, fmt.Errorf("create trigger: enqueue push: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("create trigger: commit", "device_id", deviceID, "error", err)
		return Trigger{}, fmt.Errorf("create trigger: commit: %w", err)
	}
	return t, nil
}

// List returns non-deleted triggers for the device.
func (s *Service) List(ctx context.Context, deviceID, userID string) ([]Trigger, error) {
	triggers, err := s.store.List(ctx, deviceID, userID)
	if err != nil {
		s.logger.Error("list triggers", "device_id", deviceID, "error", err)
	}
	return triggers, err
}

// Get returns a single trigger by ID.
func (s *Service) Get(ctx context.Context, deviceID, userID, triggerID string) (Trigger, error) {
	t, err := s.store.Get(ctx, deviceID, userID, triggerID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		s.logger.Error("get trigger", "device_id", deviceID, "trigger_id", triggerID, "error", err)
	}
	return t, err
}

// Update applies partial changes to a trigger and enqueues a trigger.push outbox event atomically.
func (s *Service) Update(ctx context.Context, deviceID, userID, triggerID string, patch TriggerPatch) (Trigger, error) {
	current, err := s.store.Get(ctx, deviceID, userID, triggerID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Error("update trigger: get current", "device_id", deviceID, "trigger_id", triggerID, "error", err)
		}
		return Trigger{}, err
	}

	merged := TriggerUpdate{
		Name:            current.Name,
		Condition:       current.Condition,
		Action:          current.Action,
		CooldownSeconds: current.CooldownSeconds,
		Enabled:         current.Enabled,
	}
	if patch.Name != nil {
		merged.Name = *patch.Name
	}
	if len(patch.Condition) > 0 {
		merged.Condition = patch.Condition
	}
	if len(patch.Action) > 0 {
		merged.Action = patch.Action
	}
	if patch.CooldownSeconds != nil {
		merged.CooldownSeconds = *patch.CooldownSeconds
	}
	if patch.Enabled != nil {
		merged.Enabled = *patch.Enabled
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Trigger{}, fmt.Errorf("update trigger: begin tx: %w", err)
	}
	defer tx.Rollback()

	updated, kindPin, err := s.store.Update(ctx, tx, deviceID, userID, triggerID, merged)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Error("update trigger: store", "device_id", deviceID, "trigger_id", triggerID, "error", err)
		}
		return Trigger{}, err
	}

	if err := s.outbox.Insert(ctx, tx, eventTypeTriggerPush, triggerPushPayload{
		Op:               "upsert",
		ID:               updated.ID,
		DeviceID:         updated.DeviceID,
		Enabled:          updated.Enabled,
		Condition:        json.RawMessage(updated.Condition),
		TargetPeripheral: kindPin,
		Action:           json.RawMessage(updated.Action),
		CooldownS:        updated.CooldownSeconds,
	}, triggerPushClaimTimeoutSeconds); err != nil {
		s.logger.Error("update trigger: enqueue push", "device_id", deviceID, "trigger_id", triggerID, "error", err)
		return Trigger{}, fmt.Errorf("update trigger: enqueue push: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("update trigger: commit", "device_id", deviceID, "trigger_id", triggerID, "error", err)
		return Trigger{}, fmt.Errorf("update trigger: commit: %w", err)
	}
	return updated, nil
}

// Delete soft-deletes a trigger and enqueues a trigger.push delete outbox event atomically.
func (s *Service) Delete(ctx context.Context, deviceID, userID, triggerID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete trigger: begin tx: %w", err)
	}
	defer tx.Rollback()

	deleted, err := s.store.Delete(ctx, tx, deviceID, userID, triggerID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Error("delete trigger: store", "device_id", deviceID, "trigger_id", triggerID, "error", err)
		}
		return err
	}

	if err := s.outbox.Insert(ctx, tx, eventTypeTriggerPush, triggerPushPayload{
		Op:       "delete",
		ID:       deleted.ID,
		DeviceID: deleted.DeviceID,
	}, triggerPushClaimTimeoutSeconds); err != nil {
		s.logger.Error("delete trigger: enqueue push", "device_id", deviceID, "trigger_id", triggerID, "error", err)
		return fmt.Errorf("delete trigger: enqueue push: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("delete trigger: commit", "device_id", deviceID, "trigger_id", triggerID, "error", err)
		return fmt.Errorf("delete trigger: commit: %w", err)
	}
	return nil
}

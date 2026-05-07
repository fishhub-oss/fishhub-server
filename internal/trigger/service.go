package trigger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

const eventTypeTriggerPush = "trigger.push"
const triggerPushClaimTimeoutSeconds = 30

type actionPayload struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

type triggerPushPayload struct {
	Op        string          `json:"op"`
	ID        string          `json:"id"`
	DeviceID  string          `json:"device_id"`
	Enabled   bool            `json:"enabled,omitempty"`
	Condition json.RawMessage `json:"condition,omitempty"`
	Actions   []actionPayload `json:"actions,omitempty"`
	CooldownS int             `json:"cooldown_s,omitempty"`
}

// PeripheralQuerier is the minimal interface Service needs to validate peripheral ownership.
type PeripheralQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
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

	for i, a := range p.Actions {
		if a.Type != "peripheral_action" {
			continue
		}
		kindPin, err := s.validatePeripheralAction(ctx, tx, deviceID, userID, a)
		if err != nil {
			return Trigger{}, err
		}
		cfg, err := buildPeripheralActionConfig(a.Config, kindPin)
		if err != nil {
			return Trigger{}, fmt.Errorf("create trigger: build action config: %w", err)
		}
		p.Actions[i].Config = cfg
	}

	t, err := s.store.Create(ctx, tx, deviceID, userID, p)
	if err != nil {
		return Trigger{}, err
	}

	if err := s.outbox.Insert(ctx, tx, eventTypeTriggerPush, triggerPushPayload{
		Op:        "upsert",
		ID:        t.ID,
		DeviceID:  t.DeviceID,
		Enabled:   t.Enabled,
		Condition: json.RawMessage(t.Condition),
		Actions:   actionsToPayload(t.Actions),
		CooldownS: t.CooldownSeconds,
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Trigger{}, fmt.Errorf("update trigger: begin tx: %w", err)
	}
	defer tx.Rollback()

	merged, err := s.applyPatch(ctx, tx, deviceID, userID, current, patch)
	if err != nil {
		return Trigger{}, err
	}

	updated, err := s.store.Update(ctx, tx, deviceID, userID, triggerID, merged)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.logger.Error("update trigger: store", "device_id", deviceID, "trigger_id", triggerID, "error", err)
		}
		return Trigger{}, err
	}

	if err := s.outbox.Insert(ctx, tx, eventTypeTriggerPush, triggerPushPayload{
		Op:        "upsert",
		ID:        updated.ID,
		DeviceID:  updated.DeviceID,
		Enabled:   updated.Enabled,
		Condition: json.RawMessage(updated.Condition),
		Actions:   actionsToPayload(updated.Actions),
		CooldownS: updated.CooldownSeconds,
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

// applyPatch merges a TriggerPatch onto current, returning the fully-merged TriggerUpdate.
// If the action is being changed, validatePeripheralAction is called to resolve the kind-pin
// and inject it into the config before storing.
func (s *Service) applyPatch(ctx context.Context, tx *sql.Tx, deviceID, userID string, current Trigger, patch TriggerPatch) (TriggerUpdate, error) {
	u := TriggerUpdate{
		Name:            current.Name,
		Condition:       current.Condition,
		Actions:         current.Actions,
		CooldownSeconds: current.CooldownSeconds,
		Enabled:         current.Enabled,
	}
	if patch.Name != nil {
		u.Name = *patch.Name
	}
	if len(patch.Condition) > 0 {
		u.Condition = patch.Condition
	}
	if patch.CooldownSeconds != nil {
		u.CooldownSeconds = *patch.CooldownSeconds
	}
	if patch.Enabled != nil {
		u.Enabled = *patch.Enabled
	}
	if patch.Actions != nil {
		resolved := make([]Action, len(patch.Actions))
		for i, a := range patch.Actions {
			if a.Type != "peripheral_action" {
				resolved[i] = a
				continue
			}
			kindPin, err := s.validatePeripheralAction(ctx, tx, deviceID, userID, a)
			if err != nil {
				return TriggerUpdate{}, err
			}
			cfg, err := buildPeripheralActionConfig(a.Config, kindPin)
			if err != nil {
				return TriggerUpdate{}, fmt.Errorf("apply patch: build action config: %w", err)
			}
			resolved[i] = Action{Type: a.Type, Config: cfg}
		}
		u.Actions = resolved
	}
	return u, nil
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

// validatePeripheralAction checks that the peripheral in action.Config is an actuator
// owned by deviceID/userID, and returns its resolved kind-pin string.
func (s *Service) validatePeripheralAction(ctx context.Context, q PeripheralQuerier, deviceID, userID string, action Action) (string, error) {
	var cfg struct {
		PeripheralID string `json:"peripheral_id"`
	}
	if err := json.Unmarshal(action.Config, &cfg); err != nil || cfg.PeripheralID == "" {
		return "", ErrInvalidPeripheral
	}

	var kindPin string
	err := q.QueryRowContext(ctx, `
		SELECT concat(pr.kind, '-', pr.pin::text)
		FROM peripherals pr
		JOIN devices d ON d.id = pr.device_id
		WHERE pr.id = $1
		  AND pr.device_id = $2
		  AND d.user_id = $3
		  AND pr.category = 'actuator'
		  AND pr.deleted_at IS NULL
		  AND d.deleted_at IS NULL
	`, cfg.PeripheralID, deviceID, userID).Scan(&kindPin)
	if errors.Is(err, sql.ErrNoRows) {
		var deviceExists bool
		_ = q.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)`,
			deviceID, userID,
		).Scan(&deviceExists)
		if !deviceExists {
			return "", device.ErrNotFound
		}
		return "", ErrInvalidPeripheral
	}
	if err != nil {
		return "", fmt.Errorf("validate peripheral action: %w", err)
	}
	return kindPin, nil
}

// buildPeripheralActionConfig injects the resolved kind-pin into the action config JSONB.
func buildPeripheralActionConfig(raw json.RawMessage, kindPin string) (json.RawMessage, error) {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	peripheralBytes, err := json.Marshal(kindPin)
	if err != nil {
		return nil, err
	}
	cfg["peripheral"] = peripheralBytes
	return json.Marshal(cfg)
}

func actionsToPayload(actions []Action) []actionPayload {
	out := make([]actionPayload, len(actions))
	for i, a := range actions {
		out[i] = actionPayload{Type: a.Type, Config: a.Config}
	}
	return out
}

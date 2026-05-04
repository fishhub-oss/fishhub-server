package trigger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fishhub-oss/fishhub-server/internal/device"
)

// Store handles trigger persistence.
type Store interface {
	// Create inserts a new trigger for deviceID owned by userID.
	// Validates that target_peripheral_id refers to an actuator peripheral on the same device.
	// Returns device.ErrNotFound if the device does not exist or is not owned by userID.
	// Returns ErrInvalidPeripheral if the peripheral is missing, deleted, or not an actuator.
	// Returns (trigger, peripheralKindPin, error).
	Create(ctx context.Context, tx *sql.Tx, deviceID, userID string, p TriggerCreate) (Trigger, string, error)
	// List returns non-deleted triggers for the device owned by userID.
	List(ctx context.Context, deviceID, userID string) ([]Trigger, error)
	// Get returns a single non-deleted trigger. Returns ErrNotFound if not reachable.
	Get(ctx context.Context, deviceID, userID, triggerID string) (Trigger, error)
	// Update replaces trigger fields within tx, joining peripherals to return kind-pin.
	// Returns ErrNotFound if the trigger does not exist or is not reachable by userID.
	// Returns (trigger, peripheralKindPin, error).
	Update(ctx context.Context, tx *sql.Tx, deviceID, userID, triggerID string, u TriggerUpdate) (Trigger, string, error)
	// Delete soft-deletes the trigger (sets deleted_at). Returns ErrNotFound if not reachable.
	Delete(ctx context.Context, tx *sql.Tx, deviceID, userID, triggerID string) (Trigger, error)
}

type postgresStore struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return &postgresStore{db: db}
}

func (s *postgresStore) Create(ctx context.Context, tx *sql.Tx, deviceID, userID string, p TriggerCreate) (Trigger, string, error) {
	var kindPin string
	err := tx.QueryRowContext(ctx, `
		SELECT concat(pr.kind, '-', pr.pin::text)
		FROM peripherals pr
		JOIN devices d ON d.id = pr.device_id
		WHERE pr.id = $1
		  AND pr.device_id = $2
		  AND d.user_id = $3
		  AND pr.category = 'actuator'
		  AND pr.deleted_at IS NULL
		  AND d.deleted_at IS NULL
	`, p.TargetPeripheralID, deviceID, userID).Scan(&kindPin)
	if errors.Is(err, sql.ErrNoRows) {
		var deviceExists bool
		_ = tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)`,
			deviceID, userID,
		).Scan(&deviceExists)
		if !deviceExists {
			return Trigger{}, "", device.ErrNotFound
		}
		return Trigger{}, "", ErrInvalidPeripheral
	}
	if err != nil {
		return Trigger{}, "", fmt.Errorf("create trigger: resolve peripheral: %w", err)
	}

	var t Trigger
	err = tx.QueryRowContext(ctx, `
		INSERT INTO triggers (device_id, name, condition, target_peripheral_id, action, cooldown_s)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, device_id, name, enabled, condition, target_peripheral_id, action, cooldown_s, created_at
	`, deviceID, p.Name, p.Condition, p.TargetPeripheralID, p.Action, p.CooldownSeconds).Scan(
		&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
		&t.Condition, &t.TargetPeripheralID, &t.Action, &t.CooldownSeconds, &t.CreatedAt,
	)
	if err != nil {
		return Trigger{}, "", fmt.Errorf("create trigger: insert: %w", err)
	}
	return t, kindPin, nil
}

func (s *postgresStore) List(ctx context.Context, deviceID, userID string) ([]Trigger, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tr.id, tr.device_id, tr.name, tr.enabled, tr.condition,
		       tr.target_peripheral_id, tr.action, tr.cooldown_s, tr.created_at
		FROM triggers tr
		JOIN devices d ON d.id = tr.device_id
		WHERE tr.device_id = $1
		  AND d.user_id = $2
		  AND tr.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		ORDER BY tr.created_at ASC
	`, deviceID, userID)
	if err != nil {
		return nil, fmt.Errorf("list triggers: query: %w", err)
	}
	defer rows.Close()

	triggers := []Trigger{}
	for rows.Next() {
		var t Trigger
		if err := rows.Scan(
			&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
			&t.Condition, &t.TargetPeripheralID, &t.Action, &t.CooldownSeconds, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("list triggers: scan: %w", err)
		}
		triggers = append(triggers, t)
	}
	return triggers, rows.Err()
}

func (s *postgresStore) Get(ctx context.Context, deviceID, userID, triggerID string) (Trigger, error) {
	var t Trigger
	err := s.db.QueryRowContext(ctx, `
		SELECT tr.id, tr.device_id, tr.name, tr.enabled, tr.condition,
		       tr.target_peripheral_id, tr.action, tr.cooldown_s, tr.created_at
		FROM triggers tr
		JOIN devices d ON d.id = tr.device_id
		WHERE tr.id = $1
		  AND tr.device_id = $2
		  AND d.user_id = $3
		  AND tr.deleted_at IS NULL
		  AND d.deleted_at IS NULL
	`, triggerID, deviceID, userID).Scan(
		&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
		&t.Condition, &t.TargetPeripheralID, &t.Action, &t.CooldownSeconds, &t.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Trigger{}, ErrNotFound
	}
	if err != nil {
		return Trigger{}, fmt.Errorf("get trigger: %w", err)
	}
	return t, nil
}

func (s *postgresStore) Update(ctx context.Context, tx *sql.Tx, deviceID, userID, triggerID string, u TriggerUpdate) (Trigger, string, error) {
	var t Trigger
	var kindPin string
	err := tx.QueryRowContext(ctx, `
		UPDATE triggers tr
		SET name       = $1,
		    condition  = $2,
		    action     = $3,
		    cooldown_s = $4,
		    enabled    = $5
		FROM devices d,
		     peripherals pr
		WHERE tr.id        = $6
		  AND tr.device_id = $7
		  AND d.id         = tr.device_id
		  AND d.user_id    = $8
		  AND pr.id        = tr.target_peripheral_id
		  AND tr.deleted_at IS NULL
		  AND d.deleted_at  IS NULL
		RETURNING tr.id, tr.device_id, tr.name, tr.enabled, tr.condition,
		          tr.target_peripheral_id, tr.action, tr.cooldown_s, tr.created_at,
		          concat(pr.kind, '-', pr.pin::text)
	`, u.Name, u.Condition, u.Action, u.CooldownSeconds, u.Enabled,
		triggerID, deviceID, userID,
	).Scan(
		&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
		&t.Condition, &t.TargetPeripheralID, &t.Action, &t.CooldownSeconds, &t.CreatedAt,
		&kindPin,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Trigger{}, "", ErrNotFound
	}
	if err != nil {
		return Trigger{}, "", fmt.Errorf("update trigger: %w", err)
	}
	return t, kindPin, nil
}

func (s *postgresStore) Delete(ctx context.Context, tx *sql.Tx, deviceID, userID, triggerID string) (Trigger, error) {
	var t Trigger
	err := tx.QueryRowContext(ctx, `
		UPDATE triggers tr
		SET deleted_at = now()
		FROM devices d
		WHERE tr.id        = $1
		  AND tr.device_id = $2
		  AND d.id         = tr.device_id
		  AND d.user_id    = $3
		  AND tr.deleted_at IS NULL
		  AND d.deleted_at  IS NULL
		RETURNING tr.id, tr.device_id, tr.name, tr.enabled, tr.condition,
		          tr.target_peripheral_id, tr.action, tr.cooldown_s, tr.created_at
	`, triggerID, deviceID, userID).Scan(
		&t.ID, &t.DeviceID, &t.Name, &t.Enabled,
		&t.Condition, &t.TargetPeripheralID, &t.Action, &t.CooldownSeconds, &t.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Trigger{}, ErrNotFound
	}
	if err != nil {
		return Trigger{}, fmt.Errorf("delete trigger: %w", err)
	}
	return t, nil
}

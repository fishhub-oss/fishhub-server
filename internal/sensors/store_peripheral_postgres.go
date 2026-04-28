package sensors

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type postgresPeripheralStore struct {
	db *sql.DB
}

func NewPeripheralStore(db *sql.DB) PeripheralStore {
	return &postgresPeripheralStore{db: db}
}

func (s *postgresPeripheralStore) CreatePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, name, kind string, pin int) (Peripheral, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM devices WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL)`,
		deviceID, userID,
	).Scan(&exists)
	if err != nil {
		return Peripheral{}, fmt.Errorf("create peripheral: check device: %w", err)
	}
	if !exists {
		return Peripheral{}, ErrDeviceNotFound
	}

	var p Peripheral
	err = tx.QueryRowContext(ctx, `
		INSERT INTO peripherals (device_id, name, kind, pin)
		VALUES ($1, $2, $3, $4)
		RETURNING id, device_id, name, kind, pin, created_at, updated_at
	`, deviceID, name, kind, pin).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return Peripheral{}, ErrPeripheralAlreadyExists
		}
		return Peripheral{}, fmt.Errorf("create peripheral: insert: %w", err)
	}
	return p, nil
}

func (s *postgresPeripheralStore) ListPeripherals(ctx context.Context, deviceID, userID string) ([]Peripheral, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.device_id, p.name, p.kind, p.pin, p.schedule, p.created_at, p.updated_at
		FROM peripherals p
		JOIN devices d ON d.id = p.device_id
		WHERE p.device_id = $1
		  AND d.user_id = $2
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		ORDER BY p.created_at ASC
	`, deviceID, userID)
	if err != nil {
		return nil, fmt.Errorf("list peripherals: query: %w", err)
	}
	defer rows.Close()

	peripherals := []Peripheral{}
	for rows.Next() {
		var p Peripheral
		var scheduleJSON []byte
		if err := rows.Scan(&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin, &scheduleJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("list peripherals: scan: %w", err)
		}
		if scheduleJSON != nil {
			if err := json.Unmarshal(scheduleJSON, &p.Schedule); err != nil {
				return nil, fmt.Errorf("list peripherals: unmarshal schedule: %w", err)
			}
		}
		peripherals = append(peripherals, p)
	}
	return peripherals, rows.Err()
}

func (s *postgresPeripheralStore) SetPeripheralSchedule(ctx context.Context, deviceID, userID, name string, schedule []ScheduleWindow) (Peripheral, error) {
	scheduleJSON, err := json.Marshal(schedule)
	if err != nil {
		return Peripheral{}, fmt.Errorf("set peripheral schedule: marshal: %w", err)
	}

	var p Peripheral
	err = s.db.QueryRowContext(ctx, `
		UPDATE peripherals p
		SET schedule = $1, updated_at = now()
		FROM devices d
		WHERE p.device_id = d.id
		  AND d.user_id = $2
		  AND p.device_id = $3
		  AND p.name = $4
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		RETURNING p.id, p.device_id, p.name, p.kind, p.pin, p.schedule, p.created_at, p.updated_at
	`, scheduleJSON, userID, deviceID, name).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin, &scheduleJSON, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Peripheral{}, ErrPeripheralNotFound
	}
	if err != nil {
		return Peripheral{}, fmt.Errorf("set peripheral schedule: update: %w", err)
	}
	if err := json.Unmarshal(scheduleJSON, &p.Schedule); err != nil {
		return Peripheral{}, fmt.Errorf("set peripheral schedule: unmarshal: %w", err)
	}
	return p, nil
}

func (s *postgresPeripheralStore) DeletePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, name string) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE peripherals p
		SET deleted_at = now()
		FROM devices d
		WHERE p.device_id = d.id
		  AND d.user_id = $1
		  AND p.device_id = $2
		  AND p.name = $3
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
	`, userID, deviceID, name)
	if err != nil {
		return fmt.Errorf("delete peripheral: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete peripheral: rows affected: %w", err)
	}
	if n == 0 {
		return ErrPeripheralNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}

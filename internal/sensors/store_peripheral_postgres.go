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

func (s *postgresPeripheralStore) CreatePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, name, kind, category string, pin int) (Peripheral, error) {
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
	var controlMode sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO peripherals (device_id, name, kind, pin, category, control_mode)
		VALUES ($1, $2, $3, $4, $5,
			CASE WHEN $5 = 'actuator' THEN 'automatic' ELSE NULL END)
		RETURNING id, device_id, name, kind, pin, category, control_mode, created_at, updated_at
	`, deviceID, name, kind, pin, category).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		switch uniqueViolationIndex(err) {
		case "peripherals_device_name_active_idx":
			return Peripheral{}, ErrPeripheralAlreadyExists
		case "peripherals_device_pin_active_idx":
			return Peripheral{}, ErrPeripheralPinInUse
		case "":
		default:
			return Peripheral{}, ErrPeripheralAlreadyExists
		}
		return Peripheral{}, fmt.Errorf("create peripheral: insert: %w", err)
	}
	if controlMode.Valid {
		p.ControlMode = &controlMode.String
	}
	return p, nil
}

func (s *postgresPeripheralStore) ListPeripherals(ctx context.Context, deviceID, userID string) ([]Peripheral, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.device_id, p.name, p.kind, p.pin, p.category, p.control_mode,
		       p.schedule, p.created_at, p.updated_at
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
		var controlMode sql.NullString
		var scheduleJSON []byte
		if err := rows.Scan(
			&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
			&p.Category, &controlMode, &scheduleJSON,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("list peripherals: scan: %w", err)
		}
		if controlMode.Valid {
			p.ControlMode = &controlMode.String
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
	var controlMode sql.NullString
	var scheduleOut []byte
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
		RETURNING p.id, p.device_id, p.name, p.kind, p.pin, p.category, p.control_mode,
		          p.schedule, p.created_at, p.updated_at
	`, scheduleJSON, userID, deviceID, name).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &scheduleOut,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Peripheral{}, ErrPeripheralNotFound
	}
	if err != nil {
		return Peripheral{}, fmt.Errorf("set peripheral schedule: update: %w", err)
	}
	if controlMode.Valid {
		p.ControlMode = &controlMode.String
	}
	if err := json.Unmarshal(scheduleOut, &p.Schedule); err != nil {
		return Peripheral{}, fmt.Errorf("set peripheral schedule: unmarshal: %w", err)
	}
	return p, nil
}

func (s *postgresPeripheralStore) SetControlMode(ctx context.Context, tx *sql.Tx, deviceID, userID, name, mode string) (Peripheral, error) {
	var p Peripheral
	var controlMode sql.NullString
	var scheduleJSON []byte
	err := tx.QueryRowContext(ctx, `
		UPDATE peripherals p
		SET control_mode = $1, updated_at = now()
		FROM devices d
		WHERE p.device_id = d.id
		  AND d.user_id = $2
		  AND p.device_id = $3
		  AND p.name = $4
		  AND p.category = 'actuator'
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		RETURNING p.id, p.device_id, p.name, p.kind, p.pin, p.category, p.control_mode,
		          p.schedule, p.created_at, p.updated_at
	`, mode, userID, deviceID, name).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &scheduleJSON,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		// Distinguish "not an actuator" from "not found".
		var category string
		lookupErr := s.db.QueryRowContext(ctx, `
			SELECT p.category
			FROM peripherals p
			JOIN devices d ON d.id = p.device_id
			WHERE p.device_id = $1
			  AND d.user_id = $2
			  AND p.name = $3
			  AND p.deleted_at IS NULL
			  AND d.deleted_at IS NULL
		`, deviceID, userID, name).Scan(&category)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return Peripheral{}, ErrPeripheralNotFound
		}
		if lookupErr != nil {
			return Peripheral{}, fmt.Errorf("set control mode: lookup: %w", lookupErr)
		}
		return Peripheral{}, ErrNotAnActuator
	}
	if err != nil {
		return Peripheral{}, fmt.Errorf("set control mode: update: %w", err)
	}
	if controlMode.Valid {
		p.ControlMode = &controlMode.String
	}
	if scheduleJSON != nil {
		if err := json.Unmarshal(scheduleJSON, &p.Schedule); err != nil {
			return Peripheral{}, fmt.Errorf("set control mode: unmarshal schedule: %w", err)
		}
	}
	return p, nil
}

func (s *postgresPeripheralStore) DeletePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, name string) (Peripheral, error) {
	var p Peripheral
	var controlMode sql.NullString
	var schedule []byte
	err := tx.QueryRowContext(ctx, `
		UPDATE peripherals p
		SET deleted_at = now()
		FROM devices d
		WHERE p.device_id = d.id
		  AND d.user_id = $1
		  AND p.device_id = $2
		  AND p.name = $3
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		RETURNING p.id, p.device_id, p.name, p.kind, p.pin, p.category, p.control_mode, p.schedule, p.created_at, p.updated_at
	`, userID, deviceID, name).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &schedule, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Peripheral{}, ErrPeripheralNotFound
	}
	if err != nil {
		return Peripheral{}, fmt.Errorf("delete peripheral: %w", err)
	}
	if controlMode.Valid {
		p.ControlMode = &controlMode.String
	}
	if err := json.Unmarshal(schedule, &p.Schedule); err != nil {
		p.Schedule = []ScheduleWindow{}
	}
	return p, nil
}

// uniqueViolationIndex returns the index name from a Postgres unique-violation
// error (SQLSTATE 23505), or "" if the error is not a unique violation.
func uniqueViolationIndex(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if !strings.Contains(msg, "23505") {
		return ""
	}
	const marker = `unique constraint "`
	idx := strings.Index(msg, marker)
	if idx < 0 {
		return "unknown"
	}
	rest := msg[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "unknown"
	}
	return rest[:end]
}

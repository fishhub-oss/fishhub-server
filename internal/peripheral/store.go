package peripheral

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/devicemodel"
)

type Store interface {
	// CreatePeripheral inserts a new peripheral for the device owned by userID.
	// port must belong to the device's model and match kind; its pin is used for the peripheral.
	// Returns device.ErrNotFound if the device does not exist or is not owned by userID.
	// Returns ErrAlreadyExists if an active peripheral with the same name exists.
	// Returns ErrPortInUse if an active peripheral already occupies the port.
	// category must be "sensor" or "actuator"; actuators get control_mode defaulted to "automatic".
	CreatePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, name, kind, category string, purpose *string, port devicemodel.Port) (Peripheral, error)
	// ListPeripherals returns active (non-deleted) peripherals for the device owned by userID,
	// each joined with its port (label, pin) when port_id is set.
	// Returns an empty slice if the device does not exist or is not owned by userID.
	ListPeripherals(ctx context.Context, deviceID, userID string) ([]Peripheral, error)
	// GetPeripheral returns a single active peripheral by ID, scoped to the device and user.
	// Returns ErrNotFound if it does not exist or is not reachable by userID.
	GetPeripheral(ctx context.Context, deviceID, userID, peripheralID string) (Peripheral, error)
	// SetPeripheralSchedule persists the schedule and returns the updated peripheral.
	// Returns ErrNotFound if the peripheral does not exist or is not reachable by userID.
	SetPeripheralSchedule(ctx context.Context, deviceID, userID, peripheralID string, schedule []ScheduleWindow) (Peripheral, error)
	// SetControlMode updates control_mode for an actuator peripheral within the provided transaction.
	// Returns ErrNotFound if the peripheral does not exist or is not reachable by userID.
	// Returns ErrNotAnActuator if the peripheral's category is not "actuator".
	SetControlMode(ctx context.Context, tx *sql.Tx, deviceID, userID, peripheralID, mode string) (Peripheral, error)
	// UpdatePeripheral updates name on an active peripheral owned by userID.
	// Returns ErrNotFound if the peripheral does not exist or is not reachable by userID.
	// Returns ErrAlreadyExists if another active peripheral on the device has the same name.
	UpdatePeripheral(ctx context.Context, deviceID, userID, peripheralID, name string) (Peripheral, error)
	// DeletePeripheral soft-deletes the peripheral (sets deleted_at) and returns it.
	// Returns ErrNotFound if the peripheral does not exist or is not reachable by userID.
	DeletePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, peripheralID string) (Peripheral, error)
}

type postgresStore struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return &postgresStore{db: db}
}

func (s *postgresStore) CreatePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, name, kind, category string, purpose *string, port devicemodel.Port) (Peripheral, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM devices WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL)`,
		deviceID, userID,
	).Scan(&exists)
	if err != nil {
		return Peripheral{}, fmt.Errorf("create peripheral: check device: %w", err)
	}
	if !exists {
		return Peripheral{}, device.ErrNotFound
	}

	var p Peripheral
	var controlMode sql.NullString
	var purposeOut sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO peripherals (device_id, name, kind, pin, port_id, category, control_mode, purpose)
		VALUES ($1, $2, $3, $4, $5, $6,
			CASE WHEN $6 = 'actuator' THEN 'automatic' ELSE NULL END,
			$7)
		RETURNING id, device_id, name, kind, pin, category, control_mode, purpose, created_at, updated_at
	`, deviceID, name, kind, port.Pin, port.ID, category, purpose).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &purposeOut, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		switch uniqueViolationIndex(err) {
		case "peripherals_device_name_active_idx":
			return Peripheral{}, ErrAlreadyExists
		case "peripherals_device_pin_active_idx":
			// Port carries the pin, so a pin conflict is always a port conflict.
			return Peripheral{}, ErrPortInUse
		case "peripherals_port_id_active_idx":
			return Peripheral{}, ErrPortInUse
		}
		return Peripheral{}, fmt.Errorf("create peripheral: insert: %w", err)
	}
	if controlMode.Valid {
		p.ControlMode = &controlMode.String
	}
	if purposeOut.Valid {
		p.Purpose = &purposeOut.String
	}
	p.Port = &Port{ID: port.ID, Label: port.Label, Pin: port.Pin}
	return p, nil
}

func (s *postgresStore) ListPeripherals(ctx context.Context, deviceID, userID string) ([]Peripheral, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.device_id, p.name, p.kind, p.pin, p.category, p.control_mode,
		       p.purpose, p.schedule, p.created_at, p.updated_at,
		       dmp.id, dmp.label, dmp.pin
		FROM peripherals p
		JOIN devices d ON d.id = p.device_id
		LEFT JOIN device_model_ports dmp ON dmp.id = p.port_id
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
		var purposeOut sql.NullString
		var scheduleJSON []byte
		var portID, portLabel sql.NullString
		var portPin sql.NullInt64
		if err := rows.Scan(
			&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
			&p.Category, &controlMode, &purposeOut, &scheduleJSON,
			&p.CreatedAt, &p.UpdatedAt,
			&portID, &portLabel, &portPin,
		); err != nil {
			return nil, fmt.Errorf("list peripherals: scan: %w", err)
		}
		if controlMode.Valid {
			p.ControlMode = &controlMode.String
		}
		if purposeOut.Valid {
			p.Purpose = &purposeOut.String
		}
		if scheduleJSON != nil {
			if err := json.Unmarshal(scheduleJSON, &p.Schedule); err != nil {
				return nil, fmt.Errorf("list peripherals: unmarshal schedule: %w", err)
			}
		}
		if portID.Valid && portLabel.Valid && portPin.Valid {
			p.Port = &Port{ID: portID.String, Label: portLabel.String, Pin: int(portPin.Int64)}
		}
		peripherals = append(peripherals, p)
	}
	return peripherals, rows.Err()
}

func (s *postgresStore) GetPeripheral(ctx context.Context, deviceID, userID, peripheralID string) (Peripheral, error) {
	var p Peripheral
	var controlMode sql.NullString
	var purposeOut sql.NullString
	var scheduleJSON []byte
	var portID, portLabel sql.NullString
	var portPin sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.device_id, p.name, p.kind, p.pin, p.category, p.control_mode,
		       p.purpose, p.schedule, p.created_at, p.updated_at,
		       dmp.id, dmp.label, dmp.pin
		FROM peripherals p
		JOIN devices d ON d.id = p.device_id
		LEFT JOIN device_model_ports dmp ON dmp.id = p.port_id
		WHERE p.device_id = $1
		  AND d.user_id = $2
		  AND p.id = $3
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
	`, deviceID, userID, peripheralID).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &purposeOut, &scheduleJSON,
		&p.CreatedAt, &p.UpdatedAt,
		&portID, &portLabel, &portPin,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Peripheral{}, ErrNotFound
	}
	if err != nil {
		return Peripheral{}, fmt.Errorf("get peripheral: %w", err)
	}
	if controlMode.Valid {
		p.ControlMode = &controlMode.String
	}
	if purposeOut.Valid {
		p.Purpose = &purposeOut.String
	}
	if scheduleJSON != nil {
		if err := json.Unmarshal(scheduleJSON, &p.Schedule); err != nil {
			return Peripheral{}, fmt.Errorf("get peripheral: unmarshal schedule: %w", err)
		}
	}
	if portID.Valid && portLabel.Valid && portPin.Valid {
		p.Port = &Port{ID: portID.String, Label: portLabel.String, Pin: int(portPin.Int64)}
	}
	return p, nil
}

func (s *postgresStore) SetPeripheralSchedule(ctx context.Context, deviceID, userID, peripheralID string, schedule []ScheduleWindow) (Peripheral, error) {
	scheduleJSON, err := json.Marshal(schedule)
	if err != nil {
		return Peripheral{}, fmt.Errorf("set peripheral schedule: marshal: %w", err)
	}

	var p Peripheral
	var controlMode sql.NullString
	var purposeOut sql.NullString
	var scheduleOut []byte
	err = s.db.QueryRowContext(ctx, `
		UPDATE peripherals p
		SET schedule = $1, updated_at = now()
		FROM devices d
		WHERE p.device_id = d.id
		  AND d.user_id = $2
		  AND p.device_id = $3
		  AND p.id = $4
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		RETURNING p.id, p.device_id, p.name, p.kind, p.pin, p.category, p.control_mode,
		          p.purpose, p.schedule, p.created_at, p.updated_at
	`, scheduleJSON, userID, deviceID, peripheralID).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &purposeOut, &scheduleOut,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Peripheral{}, ErrNotFound
	}
	if err != nil {
		return Peripheral{}, fmt.Errorf("set peripheral schedule: update: %w", err)
	}
	if controlMode.Valid {
		p.ControlMode = &controlMode.String
	}
	if purposeOut.Valid {
		p.Purpose = &purposeOut.String
	}
	if err := json.Unmarshal(scheduleOut, &p.Schedule); err != nil {
		return Peripheral{}, fmt.Errorf("set peripheral schedule: unmarshal: %w", err)
	}
	return p, nil
}

func (s *postgresStore) SetControlMode(ctx context.Context, tx *sql.Tx, deviceID, userID, peripheralID, mode string) (Peripheral, error) {
	var p Peripheral
	var controlMode sql.NullString
	var purposeOut sql.NullString
	var scheduleJSON []byte
	err := tx.QueryRowContext(ctx, `
		UPDATE peripherals p
		SET control_mode = $1, updated_at = now()
		FROM devices d
		WHERE p.device_id = d.id
		  AND d.user_id = $2
		  AND p.device_id = $3
		  AND p.id = $4
		  AND p.category = 'actuator'
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		RETURNING p.id, p.device_id, p.name, p.kind, p.pin, p.category, p.control_mode,
		          p.purpose, p.schedule, p.created_at, p.updated_at
	`, mode, userID, deviceID, peripheralID).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &purposeOut, &scheduleJSON,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		var category string
		lookupErr := s.db.QueryRowContext(ctx, `
			SELECT p.category
			FROM peripherals p
			JOIN devices d ON d.id = p.device_id
			WHERE p.device_id = $1
			  AND d.user_id = $2
			  AND p.id = $3
			  AND p.deleted_at IS NULL
			  AND d.deleted_at IS NULL
		`, deviceID, userID, peripheralID).Scan(&category)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return Peripheral{}, ErrNotFound
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
	if purposeOut.Valid {
		p.Purpose = &purposeOut.String
	}
	if scheduleJSON != nil {
		if err := json.Unmarshal(scheduleJSON, &p.Schedule); err != nil {
			return Peripheral{}, fmt.Errorf("set control mode: unmarshal schedule: %w", err)
		}
	}
	return p, nil
}

func (s *postgresStore) DeletePeripheral(ctx context.Context, tx *sql.Tx, deviceID, userID, peripheralID string) (Peripheral, error) {
	var p Peripheral
	var controlMode sql.NullString
	var purposeOut sql.NullString
	var schedule []byte
	err := tx.QueryRowContext(ctx, `
		UPDATE peripherals p
		SET deleted_at = now()
		FROM devices d
		WHERE p.device_id = d.id
		  AND d.user_id = $1
		  AND p.device_id = $2
		  AND p.id = $3
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		RETURNING p.id, p.device_id, p.name, p.kind, p.pin, p.category, p.control_mode, p.purpose, p.schedule, p.created_at, p.updated_at
	`, userID, deviceID, peripheralID).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &purposeOut, &schedule, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Peripheral{}, ErrNotFound
	}
	if err != nil {
		return Peripheral{}, fmt.Errorf("delete peripheral: %w", err)
	}
	if controlMode.Valid {
		p.ControlMode = &controlMode.String
	}
	if purposeOut.Valid {
		p.Purpose = &purposeOut.String
	}
	if err := json.Unmarshal(schedule, &p.Schedule); err != nil {
		p.Schedule = []ScheduleWindow{}
	}
	return p, nil
}

func (s *postgresStore) UpdatePeripheral(ctx context.Context, deviceID, userID, peripheralID, name string) (Peripheral, error) {
	var p Peripheral
	var controlMode sql.NullString
	var purposeOut sql.NullString
	var scheduleJSON []byte
	var portID sql.NullString
	var portLabel sql.NullString
	var portPin sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		UPDATE peripherals p
		SET name = $1, updated_at = now()
		FROM devices d
		WHERE p.device_id = d.id
		  AND d.user_id   = $2
		  AND p.device_id = $3
		  AND p.id        = $4
		  AND p.deleted_at IS NULL
		  AND d.deleted_at IS NULL
		RETURNING p.id, p.device_id, p.name, p.kind, p.pin, p.category,
		          p.control_mode, p.purpose, p.schedule, p.created_at, p.updated_at,
		          p.port_id,
		          (SELECT label FROM device_model_ports WHERE id = p.port_id),
		          (SELECT pin   FROM device_model_ports WHERE id = p.port_id)
	`, name, userID, deviceID, peripheralID).Scan(
		&p.ID, &p.DeviceID, &p.Name, &p.Kind, &p.Pin,
		&p.Category, &controlMode, &purposeOut, &scheduleJSON,
		&p.CreatedAt, &p.UpdatedAt,
		&portID, &portLabel, &portPin,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Peripheral{}, ErrNotFound
	}
	if err != nil {
		if uniqueViolationIndex(err) == "peripherals_device_name_active_idx" {
			return Peripheral{}, ErrAlreadyExists
		}
		return Peripheral{}, fmt.Errorf("update peripheral: %w", err)
	}
	if controlMode.Valid {
		p.ControlMode = &controlMode.String
	}
	if purposeOut.Valid {
		p.Purpose = &purposeOut.String
	}
	if scheduleJSON != nil {
		if err := json.Unmarshal(scheduleJSON, &p.Schedule); err != nil {
			return Peripheral{}, fmt.Errorf("update peripheral: unmarshal schedule: %w", err)
		}
	}
	if portID.Valid && portLabel.Valid && portPin.Valid {
		p.Port = &Port{ID: portID.String, Label: portLabel.String, Pin: int(portPin.Int64)}
	}
	return p, nil
}

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

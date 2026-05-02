package device

import (
	"context"
	"database/sql"
	"errors"
)

type Store interface {
	ListByUserID(ctx context.Context, userID string) ([]Device, error)
	// FindByID looks up a device by its ID regardless of owner.
	// Returns ErrNotFound if the device does not exist or is soft-deleted.
	FindByID(ctx context.Context, deviceID string) (Device, error)
	FindByIDAndUserID(ctx context.Context, deviceID, userID string) (Device, error)
	// PatchDevice updates the name of the device owned by userID.
	// Returns ErrNotFound if the device does not exist or is not owned by the user.
	PatchDevice(ctx context.Context, deviceID, userID, name string) (Device, error)
	// DeleteDevice soft-deletes the device and returns its mqtt_username for cleanup.
	// Returns ErrNotFound if the device does not exist or is not owned by the user.
	DeleteDevice(ctx context.Context, deviceID, userID string) (mqttUsername string, err error)
	// GetActivationStatus returns whether the device's MQTT credentials are ready.
	// Ready = credentials present in DB AND no pending/processing outbox event for the device.
	// Returns ErrNotFound if the device does not exist.
	GetActivationStatus(ctx context.Context, deviceID string) (ActivationStatus, error)
}

type postgresStore struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return &postgresStore{db: db}
}

func (s *postgresStore) ListByUserID(ctx context.Context, userID string) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(name, ''), created_at
		FROM devices
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	devices := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Name, &d.CreatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *postgresStore) FindByID(ctx context.Context, deviceID string) (Device, error) {
	var d Device
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, COALESCE(name, ''), created_at
		FROM devices
		WHERE id = $1 AND deleted_at IS NULL
	`, deviceID).Scan(&d.ID, &d.UserID, &d.Name, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, err
	}
	return d, nil
}

func (s *postgresStore) FindByIDAndUserID(ctx context.Context, deviceID, userID string) (Device, error) {
	var d Device
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, COALESCE(name, ''), created_at
		FROM devices
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, deviceID, userID).Scan(&d.ID, &d.UserID, &d.Name, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, err
	}
	return d, nil
}

func (s *postgresStore) DeleteDevice(ctx context.Context, deviceID, userID string) (string, error) {
	var mqttUsername string
	err := s.db.QueryRowContext(ctx, `
		UPDATE devices
		SET deleted_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
		RETURNING COALESCE(mqtt_username, '')
	`, deviceID, userID).Scan(&mqttUsername)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return mqttUsername, nil
}

func (s *postgresStore) GetActivationStatus(ctx context.Context, deviceID string) (ActivationStatus, error) {
	var username, password sql.NullString
	var pendingOutbox bool
	err := s.db.QueryRowContext(ctx, `
		SELECT
			d.mqtt_username,
			d.mqtt_password,
			EXISTS (
				SELECT 1 FROM outbox_events
				WHERE payload->>'device_id' = $1::text
				  AND status IN ('pending', 'processing')
			)
		FROM devices d
		WHERE d.id = $1::uuid AND d.deleted_at IS NULL
	`, deviceID).Scan(&username, &password, &pendingOutbox)
	if errors.Is(err, sql.ErrNoRows) {
		return ActivationStatus{}, ErrNotFound
	}
	if err != nil {
		return ActivationStatus{}, err
	}
	if !username.Valid || !password.Valid || pendingOutbox {
		return ActivationStatus{Ready: false}, nil
	}
	return ActivationStatus{
		Ready:        true,
		MQTTUsername: username.String,
		MQTTPassword: password.String,
	}, nil
}

func (s *postgresStore) PatchDevice(ctx context.Context, deviceID, userID, name string) (Device, error) {
	var d Device
	err := s.db.QueryRowContext(ctx, `
		UPDATE devices
		SET name = $1
		WHERE id = $2 AND user_id = $3 AND deleted_at IS NULL
		RETURNING id, user_id, COALESCE(name, ''), created_at
	`, name, deviceID, userID).Scan(&d.ID, &d.UserID, &d.Name, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, err
	}
	return d, nil
}

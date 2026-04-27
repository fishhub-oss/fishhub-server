package sensors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type postgresDeviceStore struct {
	db *sql.DB
}

func NewDeviceStore(db *sql.DB) DeviceStore {
	return &postgresDeviceStore{db: db}
}

func (s *postgresDeviceStore) ListByUserID(ctx context.Context, userID, _ string) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(name, ''), created_at
		FROM devices
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	devices := []Device{}
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.Name, &d.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *postgresDeviceStore) FindByIDAndUserID(ctx context.Context, deviceID, userID string) (Device, error) {
	var d Device
	err := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(name, ''), created_at
		FROM devices
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, deviceID, userID).Scan(&d.ID, &d.Name, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrDeviceNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("find device: %w", err)
	}
	return d, nil
}

func (s *postgresDeviceStore) DeleteDevice(ctx context.Context, deviceID, userID string) (string, error) {
	var mqttUsername string
	err := s.db.QueryRowContext(ctx, `
		UPDATE devices
		SET deleted_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
		RETURNING COALESCE(mqtt_username, '')
	`, deviceID, userID).Scan(&mqttUsername)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrDeviceNotFound
	}
	if err != nil {
		return "", fmt.Errorf("delete device: %w", err)
	}
	return mqttUsername, nil
}

func (s *postgresDeviceStore) PatchDevice(ctx context.Context, deviceID, userID, name string) (Device, error) {
	var d Device
	err := s.db.QueryRowContext(ctx, `
		UPDATE devices
		SET name = $1
		WHERE id = $2 AND user_id = $3 AND deleted_at IS NULL
		RETURNING id, COALESCE(name, ''), created_at
	`, name, deviceID, userID).Scan(&d.ID, &d.Name, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrDeviceNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("patch device: %w", err)
	}
	return d, nil
}


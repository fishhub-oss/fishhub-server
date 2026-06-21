package firmware

import (
	"context"
	"database/sql"
	"errors"
)

// UpdateStore persists firmware update records and the device-reported firmware version.
type UpdateStore interface {
	// SetFirmwareVersion updates devices.firmware_version for the given device.
	SetFirmwareVersion(ctx context.Context, deviceID, version string) error
	// GetFirmwareVersion returns the last-reported firmware version, or "" if unknown.
	GetFirmwareVersion(ctx context.Context, deviceID string) (string, error)
	// InsertPending creates a new pending update record.
	InsertPending(ctx context.Context, deviceID, desiredVersion, nonce string) error
	// MarkSucceeded marks the pending record as succeeded if its desired_version matches version.
	MarkSucceeded(ctx context.Context, deviceID, version string) error
	// MarkFailed marks the most-recent pending record as failed with the given reason.
	MarkFailed(ctx context.Context, deviceID, reason string) error
	// LatestRecord returns the most-recent update record for the device, or ErrRecordNotFound.
	LatestRecord(ctx context.Context, deviceID string) (UpdateRecord, error)
	// HasPending returns true if a pending record exists for this device+version.
	HasPending(ctx context.Context, deviceID, desiredVersion string) (bool, error)
	// ResetPending replaces the nonce and resets requested_at + status on the latest record.
	ResetPending(ctx context.Context, deviceID, nonce string) error
}

type postgresUpdateStore struct{ db *sql.DB }

func NewUpdateStore(db *sql.DB) UpdateStore {
	return &postgresUpdateStore{db: db}
}

func (s *postgresUpdateStore) SetFirmwareVersion(ctx context.Context, deviceID, version string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE devices SET firmware_version = $1 WHERE id = $2 AND deleted_at IS NULL`,
		version, deviceID,
	)
	return err
}

func (s *postgresUpdateStore) GetFirmwareVersion(ctx context.Context, deviceID string) (string, error) {
	var v sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT firmware_version FROM devices WHERE id = $1 AND deleted_at IS NULL`,
		deviceID,
	).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v.String, nil
}

func (s *postgresUpdateStore) InsertPending(ctx context.Context, deviceID, desiredVersion, nonce string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO device_firmware_updates (device_id, desired_version, nonce)
		VALUES ($1, $2, $3)
	`, deviceID, desiredVersion, nonce)
	return err
}

func (s *postgresUpdateStore) MarkSucceeded(ctx context.Context, deviceID, version string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE device_firmware_updates
		SET status = 'succeeded', updated_at = now()
		WHERE id = (
			SELECT id FROM device_firmware_updates
			WHERE device_id = $1 AND desired_version = $2 AND status = 'pending'
			ORDER BY created_at DESC
			LIMIT 1
		)
	`, deviceID, version)
	return err
}

func (s *postgresUpdateStore) MarkFailed(ctx context.Context, deviceID, reason string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE device_firmware_updates
		SET status = 'failed', last_error = $2, updated_at = now()
		WHERE id = (
			SELECT id FROM device_firmware_updates
			WHERE device_id = $1 AND status = 'pending'
			ORDER BY created_at DESC
			LIMIT 1
		)
	`, deviceID, reason)
	return err
}

func (s *postgresUpdateStore) LatestRecord(ctx context.Context, deviceID string) (UpdateRecord, error) {
	var r UpdateRecord
	var lastError sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, device_id, desired_version, nonce, status, COALESCE(last_error, ''), requested_at, updated_at
		FROM device_firmware_updates
		WHERE device_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, deviceID).Scan(
		&r.ID, &r.DeviceID, &r.DesiredVersion, &r.Nonce,
		&r.Status, &lastError, &r.RequestedAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return UpdateRecord{}, ErrRecordNotFound
	}
	if err != nil {
		return UpdateRecord{}, err
	}
	r.LastError = lastError.String
	return r, nil
}

func (s *postgresUpdateStore) HasPending(ctx context.Context, deviceID, desiredVersion string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM device_firmware_updates
			WHERE device_id = $1 AND desired_version = $2 AND status = 'pending'
		)
	`, deviceID, desiredVersion).Scan(&exists)
	return exists, err
}

func (s *postgresUpdateStore) ResetPending(ctx context.Context, deviceID, nonce string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE device_firmware_updates
		SET nonce = $2, status = 'pending', requested_at = now(), updated_at = now(), last_error = NULL
		WHERE id = (
			SELECT id FROM device_firmware_updates
			WHERE device_id = $1
			ORDER BY created_at DESC
			LIMIT 1
		)
	`, deviceID, nonce)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRecordNotFound
	}
	return nil
}

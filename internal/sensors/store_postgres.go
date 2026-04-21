package sensors

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

type postgresDeviceStore struct {
	db *sql.DB
}

func NewDeviceStore(db *sql.DB) DeviceStore {
	return &postgresDeviceStore{db: db}
}

func (s *postgresDeviceStore) ListByUserID(ctx context.Context, userID string) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(name, ''), created_at
		FROM devices
		WHERE user_id = $1
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

func (s *postgresDeviceStore) LookupByToken(ctx context.Context, token string) (DeviceInfo, error) {
	var info DeviceInfo
	err := s.db.QueryRowContext(ctx, `
		SELECT d.id, d.user_id
		FROM device_tokens dt
		JOIN devices d ON d.id = dt.device_id
		WHERE dt.token = $1
	`, token).Scan(&info.DeviceID, &info.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return DeviceInfo{}, ErrTokenNotFound
	}
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("lookup token: %w", err)
	}
	return info, nil
}

type postgresTokenStore struct {
	db *sql.DB
}

func NewTokenStore(db *sql.DB) TokenStore {
	return &postgresTokenStore{db: db}
}

func (s *postgresTokenStore) CreateToken(ctx context.Context, userID string) (TokenResult, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return TokenResult{}, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(raw)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TokenResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var deviceID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO devices (user_id) VALUES ($1) RETURNING id
	`, userID).Scan(&deviceID); err != nil {
		return TokenResult{}, fmt.Errorf("insert device: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO device_tokens (device_id, token) VALUES ($1, $2)
	`, deviceID, token); err != nil {
		return TokenResult{}, fmt.Errorf("insert token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return TokenResult{}, fmt.Errorf("commit tx: %w", err)
	}

	return TokenResult{Token: token, DeviceID: deviceID, UserID: userID}, nil
}

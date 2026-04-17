package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrTokenNotFound = errors.New("token not found")

type DeviceInfo struct {
	DeviceID string
	UserID   string
}

type DeviceStore interface {
	LookupByToken(ctx context.Context, token string) (DeviceInfo, error)
}

type postgresDeviceStore struct {
	db *sql.DB
}

func NewDeviceStore(db *sql.DB) DeviceStore {
	return &postgresDeviceStore{db: db}
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

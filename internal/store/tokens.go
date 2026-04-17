package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
)

type TokenResult struct {
	Token    string
	DeviceID string
	UserID   string
}

type TokenStore interface {
	CreateToken(ctx context.Context, userID string) (TokenResult, error)
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

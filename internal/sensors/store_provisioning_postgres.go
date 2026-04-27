package sensors

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
)

const provisioningCodeCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

type postgresProvisioningStore struct {
	db *sql.DB
}

func NewProvisioningStore(db *sql.DB) ProvisioningStore {
	return &postgresProvisioningStore{db: db}
}

func (s *postgresProvisioningStore) GetOrCreateCode(ctx context.Context, userID string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var code string
	err = tx.QueryRowContext(ctx, `
		SELECT code
		FROM provisioning_codes
		WHERE user_id = $1 AND used_at IS NULL
		LIMIT 1
		FOR UPDATE
	`, userID).Scan(&code)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("lookup code: %w", err)
	}

	if err == nil {
		if err := tx.Commit(); err != nil {
			return "", fmt.Errorf("commit tx: %w", err)
		}
		return code, nil
	}

	code, err = generateCode()
	if err != nil {
		return "", err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provisioning_codes (code, user_id) VALUES ($1, $2)
	`, code, userID); err != nil {
		return "", fmt.Errorf("insert provisioning code: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit tx: %w", err)
	}
	return code, nil
}

func (s *postgresProvisioningStore) ClaimCode(ctx context.Context, code string) (string, string, error) {
	var usedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT used_at FROM provisioning_codes WHERE code = $1
	`, code).Scan(&usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrCodeNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("lookup code: %w", err)
	}
	if usedAt.Valid {
		return "", "", ErrCodeAlreadyUsed
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var userID string
	if err := tx.QueryRowContext(ctx, `
		SELECT user_id FROM provisioning_codes WHERE code = $1 AND used_at IS NULL FOR UPDATE
	`, code).Scan(&userID); errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrCodeAlreadyUsed
	} else if err != nil {
		return "", "", fmt.Errorf("lock code: %w", err)
	}

	var deviceID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO devices (user_id) VALUES ($1) RETURNING id
	`, userID).Scan(&deviceID); err != nil {
		return "", "", fmt.Errorf("insert device: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE provisioning_codes SET used_at = now(), device_id = $2 WHERE code = $1
	`, code, deviceID); err != nil {
		return "", "", fmt.Errorf("claim code: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("commit tx: %w", err)
	}
	return deviceID, userID, nil
}

func (s *postgresProvisioningStore) Activate(ctx context.Context, deviceID, mqttUsername, mqttPassword string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE devices SET mqtt_username = $2, mqtt_password = $3 WHERE id = $1
	`, deviceID, mqttUsername, mqttPassword)
	return err
}

func generateCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	code := make([]byte, 6)
	for i, v := range b {
		code[i] = provisioningCodeCharset[int(v)%len(provisioningCodeCharset)]
	}
	return string(code), nil
}

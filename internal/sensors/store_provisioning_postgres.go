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

func (s *postgresProvisioningStore) GetOrCreatePending(ctx context.Context, userID string) (string, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var deviceID, code string
	err = tx.QueryRowContext(ctx, `
		SELECT d.id, pc.code
		FROM devices d
		JOIN provisioning_codes pc ON pc.device_id = d.id
		WHERE d.user_id = $1 AND d.status = 'pending'
		LIMIT 1
		FOR UPDATE
	`, userID).Scan(&deviceID, &code)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("lookup pending device: %w", err)
	}

	if err == nil {
		// existing pending device — return as-is
		if err := tx.Commit(); err != nil {
			return "", "", fmt.Errorf("commit tx: %w", err)
		}
		return deviceID, code, nil
	}

	// no pending device — create one
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO devices (user_id) VALUES ($1) RETURNING id
	`, userID).Scan(&deviceID); err != nil {
		return "", "", fmt.Errorf("insert device: %w", err)
	}

	code, err = generateCode()
	if err != nil {
		return "", "", err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provisioning_codes (code, device_id) VALUES ($1, $2)
	`, code, deviceID); err != nil {
		return "", "", fmt.Errorf("insert provisioning code: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("commit tx: %w", err)
	}
	return deviceID, code, nil
}

func (s *postgresProvisioningStore) ClaimCode(ctx context.Context, code string) (string, string, error) {
	// check existence and used state before attempting update
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

	var deviceID, userID string
	err = s.db.QueryRowContext(ctx, `
		UPDATE provisioning_codes
		SET used_at = now()
		WHERE code = $1 AND used_at IS NULL
		RETURNING device_id, (SELECT user_id FROM devices WHERE id = device_id)
	`, code).Scan(&deviceID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		// raced — another request claimed it between our SELECT and UPDATE
		return "", "", ErrCodeAlreadyUsed
	}
	if err != nil {
		return "", "", fmt.Errorf("claim code: %w", err)
	}
	return deviceID, userID, nil
}

func (s *postgresProvisioningStore) Activate(ctx context.Context, deviceID, mqttUsername, mqttPassword string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE devices SET status = 'active', mqtt_username = $2, mqtt_password = $3 WHERE id = $1
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


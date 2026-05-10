package devicemodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Store provides read access to device models and their ports.
type Store interface {
	// GetByDeviceID returns the model (with all ports) for a device owned by userID.
	// Returns ErrNotFound if the device does not exist, is not owned by userID,
	// or has no model assigned.
	GetByDeviceID(ctx context.Context, deviceID, userID string) (DeviceModel, error)

	// GetPort returns a single port by ID, verifying it belongs to the given model.
	// Returns ErrPortNotFound if the port does not exist or does not belong to the model.
	GetPort(ctx context.Context, portID, modelID string) (Port, error)

	// DefaultModelID returns the ID of the 'fishhub-v1' model.
	// Returns ErrNotFound if the seed has not run yet.
	DefaultModelID(ctx context.Context) (string, error)
}

type postgresStore struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return &postgresStore{db: db}
}

func (s *postgresStore) GetByDeviceID(ctx context.Context, deviceID, userID string) (DeviceModel, error) {
	var m DeviceModel
	err := s.db.QueryRowContext(ctx, `
		SELECT dm.id, dm.slug, dm.name, dm.created_at
		FROM device_models dm
		JOIN devices d ON d.model_id = dm.id
		WHERE d.id = $1
		  AND d.user_id = $2
		  AND d.deleted_at IS NULL
	`, deviceID, userID).Scan(&m.ID, &m.Slug, &m.Name, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DeviceModel{}, ErrNotFound
	}
	if err != nil {
		return DeviceModel{}, fmt.Errorf("get device model: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, model_id, kind, label, pin, created_at
		FROM device_model_ports
		WHERE model_id = $1
		ORDER BY kind, label
	`, m.ID)
	if err != nil {
		return DeviceModel{}, fmt.Errorf("get device model ports: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p Port
		if err := rows.Scan(&p.ID, &p.ModelID, &p.Kind, &p.Label, &p.Pin, &p.CreatedAt); err != nil {
			return DeviceModel{}, fmt.Errorf("get device model ports: scan: %w", err)
		}
		m.Ports = append(m.Ports, p)
	}
	if err := rows.Err(); err != nil {
		return DeviceModel{}, fmt.Errorf("get device model ports: iterate: %w", err)
	}
	return m, nil
}

func (s *postgresStore) GetPort(ctx context.Context, portID, modelID string) (Port, error) {
	var p Port
	err := s.db.QueryRowContext(ctx, `
		SELECT id, model_id, kind, label, pin, created_at
		FROM device_model_ports
		WHERE id = $1 AND model_id = $2
	`, portID, modelID).Scan(&p.ID, &p.ModelID, &p.Kind, &p.Label, &p.Pin, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Port{}, ErrPortNotFound
	}
	if err != nil {
		return Port{}, fmt.Errorf("get port: %w", err)
	}
	return p, nil
}

func (s *postgresStore) DefaultModelID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM device_models WHERE slug = 'fishhub-v1'`,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("default model id: %w", err)
	}
	return id, nil
}

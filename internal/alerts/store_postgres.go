package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type postgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) Store {
	return &postgresStore{db: db}
}

func (s *postgresStore) Create(ctx context.Context, a Alert) (Alert, error) {
	rawCtx, err := json.Marshal(a.Context)
	if err != nil {
		return Alert{}, fmt.Errorf("create alert: marshal context: %w", err)
	}
	var out Alert
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO alerts (user_id, device_id, trigger_id, event_id, severity, message, context)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, user_id, device_id, trigger_id, event_id, severity, message, context, created_at
	`, a.UserID, a.DeviceID, a.TriggerID, a.EventID, a.Severity, a.Message, rawCtx).Scan(
		&out.ID, &out.UserID, &out.DeviceID, &out.TriggerID, &out.EventID,
		&out.Severity, &out.Message, (*jsonMap)(&out.Context), &out.CreatedAt,
	)
	if err != nil {
		return Alert{}, fmt.Errorf("create alert: %w", err)
	}
	return out, nil
}

func (s *postgresStore) ListByUser(ctx context.Context, userID string, limit int) ([]Alert, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, device_id, trigger_id, event_id, severity, message, context, created_at
		FROM alerts
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer rows.Close()

	var out []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.DeviceID, &a.TriggerID, &a.EventID,
			&a.Severity, &a.Message, (*jsonMap)(&a.Context), &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("list alerts: scan: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		return []Alert{}, nil
	}
	return out, nil
}

// jsonMap scans a JSONB column into a map[string]any.
type jsonMap map[string]any

func (m *jsonMap) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("jsonMap: unsupported type %T", src)
	}
	return json.Unmarshal(b, m)
}

package trigger_events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type CursorPage struct {
	// AfterFiredAt and AfterID are nil on the first page.
	AfterFiredAt *time.Time
	AfterID      *string
	PageSize     int
}

type Store interface {
	Ingest(ctx context.Context, e TriggerEvent) error
	ListByTriggerCursor(ctx context.Context, triggerID string, page CursorPage) ([]TriggerEvent, error)
	TriggerBelongsToDevice(ctx context.Context, triggerID, deviceID string) (bool, error)
}

type postgresStore struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return &postgresStore{db: db}
}

func (s *postgresStore) Ingest(ctx context.Context, e TriggerEvent) error {
	readings, err := json.Marshal(e.Readings)
	if err != nil {
		return fmt.Errorf("ingest trigger event: marshal readings: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO trigger_events (trigger_event_id, trigger_id, fired_at, readings)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (trigger_event_id) DO NOTHING
	`, e.TriggerEventID, e.TriggerID, e.FiredAt, readings)
	if err != nil {
		return fmt.Errorf("ingest trigger event: %w", err)
	}
	return nil
}

func (s *postgresStore) ListByTriggerCursor(ctx context.Context, triggerID string, page CursorPage) ([]TriggerEvent, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if page.AfterFiredAt == nil || page.AfterID == nil {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, trigger_event_id, trigger_id, fired_at, received_at, readings
			FROM trigger_events
			WHERE trigger_id = $1
			ORDER BY fired_at DESC, id DESC
			LIMIT $2
		`, triggerID, page.PageSize)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, trigger_event_id, trigger_id, fired_at, received_at, readings
			FROM trigger_events
			WHERE trigger_id = $1
			  AND (fired_at, id) < ($2, $3)
			ORDER BY fired_at DESC, id DESC
			LIMIT $4
		`, triggerID, page.AfterFiredAt, page.AfterID, page.PageSize)
	}
	if err != nil {
		return nil, fmt.Errorf("list trigger events cursor: %w", err)
	}
	defer rows.Close()

	var events []TriggerEvent
	for rows.Next() {
		var e TriggerEvent
		var rawReadings []byte
		if err := rows.Scan(&e.ID, &e.TriggerEventID, &e.TriggerID, &e.FiredAt, &e.ReceivedAt, &rawReadings); err != nil {
			return nil, fmt.Errorf("list trigger events cursor: scan: %w", err)
		}
		if err := json.Unmarshal(rawReadings, &e.Readings); err != nil {
			return nil, fmt.Errorf("list trigger events cursor: unmarshal readings: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if events == nil {
		return []TriggerEvent{}, nil
	}
	return events, nil
}

func (s *postgresStore) TriggerBelongsToDevice(ctx context.Context, triggerID, deviceID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM triggers
			WHERE id = $1 AND device_id = $2 AND deleted_at IS NULL
		)
	`, triggerID, deviceID).Scan(&exists)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("trigger belongs to device: %w", err)
	}
	return exists, nil
}

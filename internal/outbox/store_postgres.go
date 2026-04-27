package outbox

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

func (s *postgresStore) ListPending(ctx context.Context, limit int) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, event_type, payload, attempts
		FROM outbox_events
		WHERE status = 'pending'
		ORDER BY created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("outbox: list pending: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.EventType, &e.Payload, &e.Attempts); err != nil {
			return nil, fmt.Errorf("outbox: scan row: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *postgresStore) MarkCompleted(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE outbox_events SET status = 'completed' WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("outbox: mark completed: %w", err)
	}
	return nil
}

func (s *postgresStore) RecordFailure(ctx context.Context, id string, attempts int, maxAttempts int, errMsg string) error {
	newAttempts := attempts + 1
	status := "pending"
	if newAttempts >= maxAttempts {
		status = "dead"
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE outbox_events
		SET attempts = $2, last_error = $3, status = $4
		WHERE id = $1
	`, id, newAttempts, errMsg, status)
	if err != nil {
		return fmt.Errorf("outbox: record failure: %w", err)
	}
	return nil
}

func (s *postgresStore) Insert(ctx context.Context, tx *sql.Tx, eventType string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox: marshal payload: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO outbox_events (event_type, payload) VALUES ($1, $2)
	`, eventType, b)
	if err != nil {
		return fmt.Errorf("outbox: insert: %w", err)
	}
	return nil
}

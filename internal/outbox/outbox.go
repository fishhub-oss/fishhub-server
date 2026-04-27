package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Event is a single row from the outbox_events table.
type Event struct {
	ID        string
	EventType string
	Payload   json.RawMessage
	Attempts  int
}

// EventProcessor handles events of a specific type.
type EventProcessor interface {
	// EventType returns the event_type string this processor handles.
	EventType() string
	// Process handles one event. Return nil to mark it completed; return an error to retry.
	Process(ctx context.Context, event Event) error
}

// Store is the persistence layer for the outbox.
type Store interface {
	// ClaimBatch atomically claims up to limit pending (or stale processing) rows,
	// setting status = 'processing' and claimed_at = now().
	// Uses SELECT FOR UPDATE SKIP LOCKED — safe for multiple concurrent runners.
	ClaimBatch(ctx context.Context, limit int) ([]Event, error)
	// MarkCompleted sets status = 'completed'.
	MarkCompleted(ctx context.Context, id string) error
	// RecordFailure increments attempts and writes the error message.
	// If the new attempts count reaches maxAttempts, sets status = 'dead'.
	RecordFailure(ctx context.Context, id string, attempts int, maxAttempts int, errMsg string) error
	// Insert adds a new pending event within the provided transaction.
	// claimTimeoutSeconds controls how long before a stale processing row is re-claimable.
	// The caller owns the transaction boundary.
	Insert(ctx context.Context, tx *sql.Tx, eventType string, payload any, claimTimeoutSeconds int) error
}

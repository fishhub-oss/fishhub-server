package account

import (
	"context"
	"database/sql"
	"errors"
)

var ErrAccountNotFound = errors.New("account not found")
var ErrInvalidTimezone = errors.New("invalid timezone")

type AccountStore interface {
	Upsert(ctx context.Context, userID, email, name string) (Account, error)
	FindByUserID(ctx context.Context, userID string) (Account, error)
	UpdateTimezone(ctx context.Context, tx *sql.Tx, userID, timezone string) (Account, error)
}

// DeviceLister lists device IDs belonging to a user. Implemented by a bridge in main.go.
type DeviceLister interface {
	ListIDsByUserID(ctx context.Context, userID string) ([]string, error)
}

package alerts

import (
	"context"
	"time"
)

type CursorPage struct {
	AfterCreatedAt *time.Time
	AfterID        *string
	PageSize       int
}

type Store interface {
	Create(ctx context.Context, a Alert) (Alert, error)
	ListByUser(ctx context.Context, userID string, limit int) ([]Alert, error)
	ListByUserCursor(ctx context.Context, userID string, page CursorPage) ([]Alert, error)
}

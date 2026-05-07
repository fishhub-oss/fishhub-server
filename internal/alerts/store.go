package alerts

import "context"

type Store interface {
	Create(ctx context.Context, a Alert) (Alert, error)
	ListByUser(ctx context.Context, userID string, limit int) ([]Alert, error)
}

package account

import (
	"context"
	"errors"
)

var ErrAccountNotFound = errors.New("account not found")

type AccountStore interface {
	Upsert(ctx context.Context, userID, email, name string) (Account, error)
	FindByUserID(ctx context.Context, userID string) (Account, error)
}

package auth

import "context"

type UserEventHandler interface {
	OnUserVerified(ctx context.Context, userID, email, name string) error
}

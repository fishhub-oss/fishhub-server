package account

import (
	"context"
	"fmt"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
)

// AccountEventHandler implements auth.UserEventHandler, bridging the auth
// domain to the account domain without creating a reverse dependency.
type AccountEventHandler struct {
	Store AccountStore
}

func (h *AccountEventHandler) OnUserVerified(ctx context.Context, userID, email, name string) error {
	if _, err := h.Store.Upsert(ctx, userID, email, name); err != nil {
		return fmt.Errorf("account event handler: %w", err)
	}
	return nil
}

// compile-time interface check
var _ auth.UserEventHandler = (*AccountEventHandler)(nil)

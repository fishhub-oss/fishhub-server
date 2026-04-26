package account

import "context"

// AccountService orchestrates account operations.
type AccountService struct {
	Store AccountStore
}

// Me returns the account for the given userID.
// Returns ErrAccountNotFound unwrapped if no account exists.
func (s *AccountService) Me(ctx context.Context, userID string) (Account, error) {
	return s.Store.FindByUserID(ctx, userID)
}

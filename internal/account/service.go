package account

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/outbox"
)

// AccountService orchestrates account operations.
type AccountService struct {
	Store       AccountStore
	DB          *sql.DB
	OutboxStore outbox.Store
	Devices     DeviceLister
}

// Me returns the account for the given userID.
// Returns ErrAccountNotFound unwrapped if no account exists.
func (s *AccountService) Me(ctx context.Context, userID string) (Account, error) {
	return s.Store.FindByUserID(ctx, userID)
}

// UpdateTimezone validates the timezone, updates accounts, and enqueues a
// device.config_push outbox event for every device owned by the user —
// all within a single DB transaction.
func (s *AccountService) UpdateTimezone(ctx context.Context, userID, timezone string) (Account, error) {
	if _, err := time.LoadLocation(timezone); err != nil {
		return Account{}, ErrInvalidTimezone
	}

	// Query device IDs outside the tx — a device created concurrently will
	// receive the push on the next timezone change, which is acceptable.
	deviceIDs, err := s.Devices.ListIDsByUserID(ctx, userID)
	if err != nil {
		return Account{}, fmt.Errorf("list devices: %w", err)
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	account, err := s.Store.UpdateTimezone(ctx, tx, userID, timezone)
	if err != nil {
		return Account{}, err
	}

	for _, deviceID := range deviceIDs {
		if err := s.OutboxStore.Insert(ctx, tx, eventTypeDeviceConfigPush,
			deviceConfigPushPayload{DeviceID: deviceID, Timezone: timezone},
			configPushClaimTimeoutSeconds,
		); err != nil {
			return Account{}, fmt.Errorf("enqueue config push for device %s: %w", deviceID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return Account{}, fmt.Errorf("commit tx: %w", err)
	}
	return account, nil
}

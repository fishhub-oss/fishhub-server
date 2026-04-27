package sensors

import (
	"context"
	"log/slog"
)

// ProvisioningService orchestrates device provisioning from the user side.
type ProvisioningService struct {
	Store  ProvisioningStore
	Logger *slog.Logger
}

// Provision returns an existing pending provisioning code for the user or
// creates a new one. The returned values are the device ID and the code.
func (s *ProvisioningService) Provision(ctx context.Context, userID string) (deviceID, code string, err error) {
	deviceID, code, err = s.Store.GetOrCreatePending(ctx, userID)
	if err != nil {
		s.Logger.Error("provision device", "user_id", userID, "error", err)
	}
	return deviceID, code, err
}

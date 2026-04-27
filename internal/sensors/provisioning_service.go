package sensors

import (
	"context"
	"log/slog"
)

// ProvisioningService orchestrates device provisioning from the user side.
type ProvisioningService struct {
	store  ProvisioningStore
	logger *slog.Logger
}

func NewProvisioningService(store ProvisioningStore, logger *slog.Logger) *ProvisioningService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProvisioningService{store: store, logger: logger}
}

// Provision returns an existing unused provisioning code for the user or
// creates a new one.
func (s *ProvisioningService) Provision(ctx context.Context, userID string) (code string, err error) {
	code, err = s.store.GetOrCreateCode(ctx, userID)
	if err != nil {
		s.logger.Error("provision: get or create code", "user_id", userID, "error", err)
	}
	return code, err
}

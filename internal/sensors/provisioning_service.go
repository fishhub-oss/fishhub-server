package sensors

import "context"

// ProvisioningService orchestrates device provisioning from the user side.
type ProvisioningService struct {
	Store ProvisioningStore
}

// Provision returns an existing pending provisioning code for the user or
// creates a new one. The returned values are the device ID and the code.
func (s *ProvisioningService) Provision(ctx context.Context, userID string) (deviceID, code string, err error) {
	return s.Store.GetOrCreatePending(ctx, userID)
}

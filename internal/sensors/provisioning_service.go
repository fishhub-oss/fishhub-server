package sensors

import (
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
)

// ProvisioningService is an alias for provisioning.Service kept for backward compatibility.
type ProvisioningService = provisioning.Service

func NewProvisioningService(store ProvisioningStore, logger *slog.Logger) *ProvisioningService {
	return provisioning.NewService(store, logger)
}

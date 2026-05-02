package sensors

import (
	"database/sql"

	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
)

// NewProvisioningStore returns a provisioning.Store backed by Postgres.
// Kept here for backward compatibility with callers that import sensors.
func NewProvisioningStore(db *sql.DB) ProvisioningStore {
	return provisioning.NewStore(db)
}

package sensors

import (
	"database/sql"

	"github.com/fishhub-oss/fishhub-server/internal/device"
)

// NewDeviceStore returns a device.Store backed by Postgres.
// Kept here for backward compatibility with callers that import sensors.
func NewDeviceStore(db *sql.DB) DeviceStore {
	return device.NewStore(db)
}

package sensors

import (
	"database/sql"

	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
)

// NewPeripheralStore returns a peripheral.Store backed by Postgres.
// Kept here for backward compatibility with callers that import sensors.
func NewPeripheralStore(db *sql.DB) PeripheralStore {
	return peripheral.NewStore(db)
}

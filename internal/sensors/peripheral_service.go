package sensors

import (
	"database/sql"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/mqtt"
	"github.com/fishhub-oss/fishhub-server/internal/outbox"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
)

// PeripheralService is an alias for peripheral.Service kept for backward compatibility.
type PeripheralService = peripheral.Service

func NewPeripheralService(
	db *sql.DB,
	store PeripheralStore,
	outboxStore outbox.Store,
	querier ReadingQuerier,
	publisher mqtt.Publisher,
	logger *slog.Logger,
) *PeripheralService {
	return peripheral.NewService(db, store, outboxStore, querier, publisher, logger)
}

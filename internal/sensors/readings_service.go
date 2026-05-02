package sensors

import (
	"context"
	"log/slog"

	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
)

// ReadingsService is an alias for measurement.ReadingsService kept for backward compatibility.
type ReadingsService = measurement.ReadingsService

// deviceStoreFinderBridge bridges device.Store (returning (Device, error)) to
// measurement.DeviceFinder (returning only error).
type deviceStoreFinderBridge struct {
	store DeviceStore
}

func (b *deviceStoreFinderBridge) FindByIDAndUserID(ctx context.Context, deviceID, userID string) error {
	_, err := b.store.FindByIDAndUserID(ctx, deviceID, userID)
	if err != nil {
		return device.ErrNotFound
	}
	return nil
}

func NewReadingsService(devices DeviceStore, querier ReadingQuerier, writer ReadingWriter, logger *slog.Logger) *ReadingsService {
	if logger == nil {
		logger = slog.Default()
	}
	return measurement.NewReadingsService(
		&deviceStoreFinderBridge{store: devices},
		querier,
		writer,
		logger,
	)
}

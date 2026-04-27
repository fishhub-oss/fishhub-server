package sensors

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ReadingsService orchestrates sensor reading operations.
type ReadingsService struct {
	devices DeviceStore
	querier ReadingQuerier
	writer  ReadingWriter
	logger  *slog.Logger
}

func NewReadingsService(devices DeviceStore, querier ReadingQuerier, writer ReadingWriter, logger *slog.Logger) *ReadingsService {
	return &ReadingsService{devices: devices, querier: querier, writer: writer, logger: logger}
}

// Query verifies device ownership then fetches readings from InfluxDB.
// Returns ErrDeviceNotFound unwrapped if the device does not exist or is not
// owned by userID.
func (s *ReadingsService) Query(ctx context.Context, userID string, q ReadingQuery) ([]ReadingPoint, error) {
	if _, err := s.devices.FindByIDAndUserID(ctx, q.DeviceID, userID); err != nil {
		return nil, err
	}
	points, err := s.querier.QueryReadings(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query readings: %w", err)
	}
	return points, nil
}

// Write parses a SenML payload and writes the reading to InfluxDB.
// If writer is nil the call is a no-op (InfluxDB not configured).
func (s *ReadingsService) Write(ctx context.Context, device DeviceInfo, body []byte) error {
	reading, err := ParseSenML(body)
	if err != nil {
		return err
	}

	s.logger.Info("reading received", "device_id", device.DeviceID, "bytes", len(body))

	if s.writer == nil {
		return nil
	}

	fields := make(map[string]any, len(reading.Measurements))
	for _, m := range reading.Measurements {
		fields[m.Name] = m.Value
	}
	if err := s.writer.WriteReading(ctx, Reading{
		DeviceID:     device.DeviceID,
		UserID:       device.UserID,
		Timestamp:    time.Unix(reading.BaseTime, 0).UTC(),
		Measurements: fields,
	}); err != nil {
		s.logger.Error("influx write", "device_id", device.DeviceID, "error", err)
		return fmt.Errorf("%w: %w", ErrInfluxWrite, err)
	}
	return nil
}

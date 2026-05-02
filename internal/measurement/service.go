package measurement

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/senml"
)

// DeviceFinder is the minimal interface ReadingsService needs to verify ownership.
// Implementations should return a sentinel error that callers can check with errors.Is.
type DeviceFinder interface {
	FindByIDAndUserID(ctx context.Context, deviceID, userID string) error
}

// ReadingsService orchestrates sensor reading operations.
type ReadingsService struct {
	devices DeviceFinder
	querier Querier
	writer  Writer
	logger  *slog.Logger
}

func NewReadingsService(devices DeviceFinder, querier Querier, writer Writer, logger *slog.Logger) *ReadingsService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReadingsService{devices: devices, querier: querier, writer: writer, logger: logger}
}

// Query verifies device ownership then fetches readings from InfluxDB.
// Returns ErrDeviceNotFound unwrapped if the device does not exist or is not owned by userID.
func (s *ReadingsService) Query(ctx context.Context, userID string, q Query) ([]Point, error) {
	if err := s.devices.FindByIDAndUserID(ctx, q.DeviceID, userID); err != nil {
		s.logger.Error("query readings: find device", "device_id", q.DeviceID, "error", err)
		return nil, err
	}
	points, err := s.querier.QueryReadings(ctx, q)
	if err != nil {
		s.logger.Error("query readings", "device_id", q.DeviceID, "error", err)
		return nil, fmt.Errorf("query readings: %w", err)
	}
	return points, nil
}

// Write parses a SenML payload and writes the reading to InfluxDB.
// If writer is nil the call is a no-op (InfluxDB not configured).
func (s *ReadingsService) Write(ctx context.Context, deviceID, userID string, body []byte) error {
	r, err := senml.Parse(body)
	if err != nil {
		return err
	}

	s.logger.Info("reading received", "device_id", deviceID, "bytes", len(body))

	if s.writer == nil {
		return nil
	}

	fields := make(map[string]any, len(r.Measurements))
	for _, m := range r.Measurements {
		fields[m.Name] = m.Value
	}
	if err := s.writer.WriteReading(ctx, Reading{
		DeviceID:     deviceID,
		UserID:       userID,
		Timestamp:    time.Unix(r.BaseTime, 0).UTC(),
		Measurements: fields,
	}); err != nil {
		s.logger.Error("influx write", "device_id", deviceID, "error", err)
		return fmt.Errorf("%w: %w", ErrInfluxWrite, err)
	}
	return nil
}

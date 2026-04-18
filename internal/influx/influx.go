package influx

import (
	"context"
	"fmt"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

// Reading represents a set of sensor measurements from a single device at a single point in time.
type Reading struct {
	DeviceID     string
	UserID       string
	Timestamp    time.Time
	Measurements map[string]any // field name → typed value (float64, bool)
}

// ReadingWriter persists sensor readings to a time-series store.
type ReadingWriter interface {
	WriteReading(ctx context.Context, r Reading) error
}

type influxDBWriter struct {
	client   *influxdb3.Client
	database string
}

// NewReadingWriter creates a ReadingWriter backed by InfluxDB 3 Core.
func NewReadingWriter(host, token, database string) (ReadingWriter, error) {
	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     host,
		Token:    token,
		Database: database,
	})
	if err != nil {
		return nil, fmt.Errorf("influx client: %w", err)
	}
	return &influxDBWriter{client: client, database: database}, nil
}

func (w *influxDBWriter) WriteReading(ctx context.Context, r Reading) error {
	tags := map[string]string{
		"device_id": r.DeviceID,
		"user_id":   r.UserID,
	}
	point := influxdb3.NewPoint("sensors", tags, r.Measurements, r.Timestamp)
	if err := w.client.WritePoints(ctx, []*influxdb3.Point{point}); err != nil {
		return fmt.Errorf("influx write: %w", err)
	}
	return nil
}

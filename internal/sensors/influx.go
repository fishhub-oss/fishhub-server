package sensors

import (
	"context"
	"fmt"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

type Reading struct {
	DeviceID     string
	UserID       string
	Timestamp    time.Time
	Measurements map[string]any
}

type ReadingWriter interface {
	WriteReading(ctx context.Context, r Reading) error
}

type influxDBWriter struct {
	client   *influxdb3.Client
	database string
}

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

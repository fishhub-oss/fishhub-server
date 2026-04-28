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

type ReadingQuery struct {
	DeviceID     string
	From         time.Time
	To           time.Time
	Window       string
	Measurements []string
}

type ReadingPoint struct {
	Timestamp time.Time
	Values    map[string]any
}

type ReadingWriter interface {
	WriteReading(ctx context.Context, r Reading) error
}

type ReadingQuerier interface {
	QueryReadings(ctx context.Context, q ReadingQuery) ([]ReadingPoint, error)
}

type InfluxClient interface {
	ReadingWriter
	ReadingQuerier
}

type influxDBClient struct {
	client   *influxdb3.Client
	database string
}

func NewInfluxClient(host, token, database string) (InfluxClient, error) {
	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     host,
		Token:    token,
		Database: database,
	})
	if err != nil {
		return nil, fmt.Errorf("influx client: %w", err)
	}
	return &influxDBClient{client: client, database: database}, nil
}

// NewReadingWriter constructs a writer-only InfluxDB client (kept for backwards compat).
func NewReadingWriter(host, token, database string) (ReadingWriter, error) {
	return NewInfluxClient(host, token, database)
}

func (c *influxDBClient) WriteReading(ctx context.Context, r Reading) error {
	tags := map[string]string{
		"device_id": r.DeviceID,
		"user_id":   r.UserID,
	}
	point := influxdb3.NewPoint("sensors", tags, r.Measurements, r.Timestamp)
	if err := c.client.WritePoints(ctx, []*influxdb3.Point{point}); err != nil {
		return fmt.Errorf("influx write: %w", err)
	}
	return nil
}

func (c *influxDBClient) QueryReadings(ctx context.Context, q ReadingQuery) ([]ReadingPoint, error) {
	// Always SELECT * — requesting specific columns fails if a field has never been
	// written to InfluxDB yet. Filter to requested measurements in Go instead.
	sql := fmt.Sprintf(
		`SELECT * FROM sensors`+
			` WHERE device_id = '%s'`+
			` AND time >= '%s'`+
			` AND time < '%s'`+
			` ORDER BY time ASC`,
		q.DeviceID,
		q.From.UTC().Format(time.RFC3339),
		q.To.UTC().Format(time.RFC3339),
	)

	// Build a set of requested measurements for O(1) lookup.
	// Empty set means return all fields.
	wantAll := len(q.Measurements) == 0
	want := make(map[string]bool, len(q.Measurements))
	for _, m := range q.Measurements {
		want[m] = true
	}

	iter, err := c.client.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("influx query: %w", err)
	}

	var points []ReadingPoint
	for iter.Next() {
		row := iter.Value()
		p := ReadingPoint{Values: make(map[string]any)}
		if t, ok := row["time"].(time.Time); ok {
			p.Timestamp = t.UTC()
		}
		for k, v := range row {
			if k == "time" || k == "device_id" || k == "user_id" {
				continue
			}
			if !wantAll && !want[k] {
				continue
			}
			switch val := v.(type) {
			case float64:
				p.Values[k] = val
			case bool:
				if val {
					p.Values[k] = 1
				} else {
					p.Values[k] = 0
				}
			case string:
				p.Values[k] = val
			}
		}
		if len(p.Values) == 0 {
			continue
		}
		points = append(points, p)
	}
	return points, nil
}

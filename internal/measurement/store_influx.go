package measurement

import (
	"context"
	"fmt"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

var reservedColumns = map[string]bool{"time": true, "device_id": true, "user_id": true}

type influxDBClient struct {
	client   *influxdb3.Client
	database string
}

// NewInfluxClient constructs a Client backed by InfluxDB 3.
func NewInfluxClient(host, token, database string) (Client, error) {
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

// QueryLastReadings returns a single Point containing the last known non-null
// value for every field the device has written recently. Returns nil if the device
// has no data within the widest lookback window.
//
// Tiered lookback: the device deep-sleeps ~5 min between cycles, so the latest
// reading is almost always within minutes. We try a short recent window first and
// only widen to a long window when it comes back empty (a peripheral that has been
// silent). Each pass is a single time-ordered top-N scan reduced in Go — far cheaper
// than an unbounded LAST_VALUE(...) OVER () window function over the whole partition.
func (c *influxDBClient) QueryLastReadings(ctx context.Context, deviceID string) (*Point, error) {
	for _, lookback := range []time.Duration{30 * time.Minute, 7 * 24 * time.Hour} {
		p, err := c.queryLastWithin(ctx, deviceID, lookback)
		if err != nil {
			return nil, err
		}
		if p != nil {
			return p, nil
		}
	}
	return nil, nil
}

// queryLastWithin scans the most recent rows within lookback, newest first, and
// keeps the first non-null value seen per field. This reproduces
// LAST_VALUE(field IGNORE NULLS) for every field independently: because rows are
// ordered time-descending, the first non-null value encountered for a field is its
// latest value. maxRows bounds the scan so a device that writes each peripheral on a
// separate cycle still terminates; in the common case (all measurements written in one
// point per cycle) the newest row already carries every field.
func (c *influxDBClient) queryLastWithin(ctx context.Context, deviceID string, lookback time.Duration) (*Point, error) {
	const maxRows = 200
	since := time.Now().UTC().Add(-lookback).Format(time.RFC3339)

	sql := fmt.Sprintf(
		`SELECT * FROM sensors WHERE device_id = '%s' AND time >= '%s' ORDER BY time DESC LIMIT %d`,
		deviceID, since, maxRows,
	)
	iter, err := c.client.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("influx query last readings: %w", err)
	}

	var rows []map[string]any
	for iter.Next() {
		rows = append(rows, iter.Value())
	}
	return reduceLatest(rows), nil
}

// reduceLatest folds rows ordered newest-first into a single Point holding the
// first non-null value seen for each field — equivalent to LAST_VALUE(field IGNORE
// NULLS) per field, since the first non-null encountered in time-descending order is
// the latest. The Point timestamp is the newest row's timestamp. Returns nil when no
// field values are present.
func reduceLatest(rows []map[string]any) *Point {
	p := &Point{Values: make(map[string]any)}
	for _, row := range rows {
		if p.Timestamp.IsZero() {
			if t, ok := row["time"].(time.Time); ok {
				p.Timestamp = t.UTC()
			}
		}
		for k, v := range row {
			if reservedColumns[k] {
				continue
			}
			if _, seen := p.Values[k]; seen {
				continue // already captured a newer (non-null) value for this field
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
	}
	if len(p.Values) == 0 {
		return nil
	}
	return p
}

func (c *influxDBClient) QueryReadings(ctx context.Context, q Query) ([]Point, error) {
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

	wantAll := len(q.Measurements) == 0
	want := make(map[string]bool, len(q.Measurements))
	for _, m := range q.Measurements {
		want[m] = true
	}

	iter, err := c.client.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("influx query: %w", err)
	}

	var points []Point
	for iter.Next() {
		row := iter.Value()
		p := Point{Values: make(map[string]any)}
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

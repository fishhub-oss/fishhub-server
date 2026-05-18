package measurement

import (
	"context"
	"fmt"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/apache/arrow-go/v18/arrow"
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
// value for every field the device has ever written. Returns nil if the device has no data.
//
// Two-step approach required by InfluxDB 3's schema-on-write model:
//  1. SELECT * LIMIT 1 to discover which field columns actually exist.
//  2. LAST_VALUE(field IGNORE NULLS) OVER () to get the latest value per field
//     independently (different peripherals write at different timestamps).
func (c *influxDBClient) QueryLastReadings(ctx context.Context, deviceID string) (*Point, error) {
	// Lookback window: covers any peripheral that has written at least once in the last 30 days.
	// Bounding the scan is critical for performance — unbounded window functions scan all history.
	const lookback = 7 * 24 * time.Hour
	since := time.Now().UTC().Add(-lookback).Format(time.RFC3339)

	discoverSQL := fmt.Sprintf(
		`SELECT * FROM sensors WHERE device_id = '%s' AND time >= '%s' ORDER BY time DESC LIMIT 1`,
		deviceID, since,
	)
	iter, err := c.client.Query(ctx, discoverSQL)
	if err != nil {
		return nil, fmt.Errorf("influx query last readings (discover): %w", err)
	}

	var fields []string
	for iter.Next() {
		for k := range iter.Value() {
			if !reservedColumns[k] {
				fields = append(fields, k)
			}
		}
		break
	}
	if len(fields) == 0 {
		return nil, nil
	}

	selectCols := ""
	for _, f := range fields {
		selectCols += fmt.Sprintf(`, LAST_VALUE("%s" IGNORE NULLS) OVER () AS "%s"`, f, f)
	}
	lastSQL := fmt.Sprintf(
		`SELECT MAX(time) OVER () AS time%s FROM sensors WHERE device_id = '%s' AND time >= '%s' LIMIT 1`,
		selectCols,
		deviceID,
		since,
	)
	iter2, err := c.client.Query(ctx, lastSQL)
	if err != nil {
		return nil, fmt.Errorf("influx query last readings: %w", err)
	}

	p := &Point{Values: make(map[string]any)}
	for iter2.Next() {
		row := iter2.Value()
		// MAX(time) OVER () returns arrow.Timestamp (nanoseconds), not time.Time.
		// The client only maps the literal "time" column to time.Time automatically.
		switch tv := row["time"].(type) {
		case time.Time:
			p.Timestamp = tv.UTC()
		case arrow.Timestamp:
			p.Timestamp = tv.ToTime(arrow.Nanosecond).UTC()
		}
		for k, v := range row {
			if reservedColumns[k] {
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
		break
	}
	if len(p.Values) == 0 {
		return nil, nil
	}
	return p, nil
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

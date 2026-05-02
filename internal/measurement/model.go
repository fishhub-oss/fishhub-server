package measurement

import (
	"context"
	"time"
)

type Reading struct {
	DeviceID     string
	UserID       string
	Timestamp    time.Time
	Measurements map[string]any
}

type Query struct {
	DeviceID     string
	From         time.Time
	To           time.Time
	Window       string
	Measurements []string
}

type Point struct {
	Timestamp time.Time
	Values    map[string]any
}

type Writer interface {
	WriteReading(ctx context.Context, r Reading) error
}

type Querier interface {
	QueryReadings(ctx context.Context, q Query) ([]Point, error)
	QueryLastReadings(ctx context.Context, deviceID string) (*Point, error)
}

type Client interface {
	Writer
	Querier
}

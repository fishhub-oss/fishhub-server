package sensors

import (
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
)

// InfluxClient combines measurement.Writer and measurement.Querier.
// Kept here for backward compatibility with main.go.
type InfluxClient = measurement.Client

// NewInfluxClient constructs a Client backed by InfluxDB 3.
// Kept here for backward compatibility with main.go.
func NewInfluxClient(host, token, database string) (InfluxClient, error) {
	return measurement.NewInfluxClient(host, token, database)
}

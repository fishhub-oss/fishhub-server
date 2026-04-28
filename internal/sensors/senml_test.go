package sensors_test

import (
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

func TestParseSenML(t *testing.T) {
	t.Run("single float measurement", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1745000000},{"n":"temperature","u":"Cel","v":25.3}]`
		r, err := sensors.ParseSenML([]byte(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.BaseTime != 1745000000 {
			t.Errorf("expected bt 1745000000, got %v", r.BaseTime)
		}
		if len(r.Measurements) != 1 {
			t.Fatalf("expected 1 measurement, got %d", len(r.Measurements))
		}
		m := r.Measurements[0]
		if m.Name != "temperature" {
			t.Errorf("expected name 'temperature', got %q", m.Name)
		}
		if v, ok := m.Value.(float64); !ok || v != 25.3 {
			t.Errorf("expected value 25.3, got %v", m.Value)
		}
		if m.Unit != "Cel" {
			t.Errorf("expected unit 'Cel', got %q", m.Unit)
		}
	})

	t.Run("multi-peripheral pack: float + bool", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1745000000},{"n":"temperature","u":"Cel","v":25.3},{"n":"relay/state","vb":true}]`
		r, err := sensors.ParseSenML([]byte(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.Measurements) != 2 {
			t.Fatalf("expected 2 measurements, got %d", len(r.Measurements))
		}
		if r.Measurements[0].Name != "temperature" {
			t.Errorf("expected first measurement 'temperature', got %q", r.Measurements[0].Name)
		}
		if r.Measurements[1].Name != "relay/state" {
			t.Errorf("expected second measurement 'relay/state', got %q", r.Measurements[1].Name)
		}
		v, ok := r.Measurements[1].Value.(bool)
		if !ok || !v {
			t.Errorf("expected bool true, got %v", r.Measurements[1].Value)
		}
	})

	t.Run("boolean-only pack", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1745000000},{"n":"relay/state","vb":false}]`
		r, err := sensors.ParseSenML([]byte(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.Measurements) != 1 {
			t.Fatalf("expected 1 measurement, got %d", len(r.Measurements))
		}
		v, ok := r.Measurements[0].Value.(bool)
		if !ok || v {
			t.Errorf("expected bool false, got %v", r.Measurements[0].Value)
		}
	})

	t.Run("string value (vs) alongside float", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1745000000},{"n":"light/source","vs":"schedule"},{"n":"temperature","v":25.3}]`
		r, err := sensors.ParseSenML([]byte(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.Measurements) != 2 {
			t.Fatalf("expected 2 measurements, got %d", len(r.Measurements))
		}
		if r.Measurements[0].Name != "light/source" {
			t.Errorf("expected first measurement 'light/source', got %q", r.Measurements[0].Name)
		}
		if v, ok := r.Measurements[0].Value.(string); !ok || v != "schedule" {
			t.Errorf("expected string 'schedule', got %v", r.Measurements[0].Value)
		}
		if r.Measurements[1].Name != "temperature" {
			t.Errorf("expected second measurement 'temperature', got %q", r.Measurements[1].Name)
		}
	})

	t.Run("records with no supported value type are skipped", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1745000000},{"n":"empty"},{"n":"temperature","v":25.3}]`
		r, err := sensors.ParseSenML([]byte(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.Measurements) != 1 {
			t.Fatalf("expected 1 measurement (empty skipped), got %d", len(r.Measurements))
		}
		if r.Measurements[0].Name != "temperature" {
			t.Errorf("expected 'temperature', got %q", r.Measurements[0].Name)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		_, err := sensors.ParseSenML([]byte(`not json`))
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty array", func(t *testing.T) {
		_, err := sensors.ParseSenML([]byte(`[]`))
		if !errors.Is(err, sensors.ErrEmptyPayload) {
			t.Errorf("expected ErrEmptyPayload, got %v", err)
		}
	})

	t.Run("single-element array (only base record)", func(t *testing.T) {
		_, err := sensors.ParseSenML([]byte(`[{"bn":"fishhub/device/","bt":1745000000}]`))
		if !errors.Is(err, sensors.ErrEmptyPayload) {
			t.Errorf("expected ErrEmptyPayload, got %v", err)
		}
	})

	t.Run("missing base time", func(t *testing.T) {
		_, err := sensors.ParseSenML([]byte(`[{"bn":"fishhub/device/"},{"n":"temperature","v":25.3}]`))
		if !errors.Is(err, sensors.ErrMissingBaseTime) {
			t.Errorf("expected ErrMissingBaseTime, got %v", err)
		}
	})

	t.Run("measurement record before base record", func(t *testing.T) {
		_, err := sensors.ParseSenML([]byte(`[{"n":"temperature","v":25.3},{"bn":"fishhub/device/","bt":1745000000}]`))
		if !errors.Is(err, sensors.ErrMissingBaseTime) {
			t.Errorf("expected ErrMissingBaseTime, got %v", err)
		}
	})

	t.Run("string-only pack parses successfully", func(t *testing.T) {
		r, err := sensors.ParseSenML([]byte(`[{"bn":"fishhub/device/","bt":1745000000},{"n":"light/source","vs":"heartbeat"}]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.Measurements) != 1 {
			t.Fatalf("expected 1 measurement, got %d", len(r.Measurements))
		}
		if v, ok := r.Measurements[0].Value.(string); !ok || v != "heartbeat" {
			t.Errorf("expected string 'heartbeat', got %v", r.Measurements[0].Value)
		}
	})

	t.Run("all measurement records have no supported value type", func(t *testing.T) {
		_, err := sensors.ParseSenML([]byte(`[{"bn":"fishhub/device/","bt":1745000000},{"n":"empty"}]`))
		if !errors.Is(err, sensors.ErrEmptyEntries) {
			t.Errorf("expected ErrEmptyEntries, got %v", err)
		}
	})
}

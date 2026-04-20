package sensors_test

import (
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

func TestParseSenML(t *testing.T) {
	t.Run("single float entry", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"temperature","u":"Cel","v":23.4}]}]`
		r, err := sensors.ParseSenML([]byte(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.BaseTime != 1713000000 {
			t.Errorf("expected bt 1713000000, got %v", r.BaseTime)
		}
		if len(r.Measurements) != 1 {
			t.Fatalf("expected 1 measurement, got %d", len(r.Measurements))
		}
		m := r.Measurements[0]
		if m.Name != "temperature" {
			t.Errorf("expected name 'temperature', got %q", m.Name)
		}
		if v, ok := m.Value.(float64); !ok || v != 23.4 {
			t.Errorf("expected value 23.4, got %v", m.Value)
		}
	})

	t.Run("multi-sensor payload", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"temperature","u":"Cel","v":23.4},{"n":"ph","u":"pH","v":7.2}]}]`
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
		if r.Measurements[1].Name != "ph" {
			t.Errorf("expected second measurement 'ph', got %q", r.Measurements[1].Name)
		}
	})

	t.Run("boolean entry", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"door_open","vb":true}]}]`
		r, err := sensors.ParseSenML([]byte(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.Measurements) != 1 {
			t.Fatalf("expected 1 measurement, got %d", len(r.Measurements))
		}
		v, ok := r.Measurements[0].Value.(bool)
		if !ok || !v {
			t.Errorf("expected bool true, got %v", r.Measurements[0].Value)
		}
	})

	t.Run("entries with unknown value type are skipped", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"label","vs":"hello"},{"n":"temperature","v":23.4}]}]`
		r, err := sensors.ParseSenML([]byte(body))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(r.Measurements) != 1 {
			t.Fatalf("expected 1 measurement (label skipped), got %d", len(r.Measurements))
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

	t.Run("missing base time", func(t *testing.T) {
		_, err := sensors.ParseSenML([]byte(`[{"bn":"fishhub/device/","e":[{"n":"temperature","v":23.4}]}]`))
		if !errors.Is(err, sensors.ErrMissingBaseTime) {
			t.Errorf("expected ErrMissingBaseTime, got %v", err)
		}
	})

	t.Run("empty entries", func(t *testing.T) {
		_, err := sensors.ParseSenML([]byte(`[{"bn":"fishhub/device/","bt":1713000000,"e":[]}]`))
		if !errors.Is(err, sensors.ErrEmptyEntries) {
			t.Errorf("expected ErrEmptyEntries, got %v", err)
		}
	})

	t.Run("all entries have no supported value type", func(t *testing.T) {
		_, err := sensors.ParseSenML([]byte(`[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"label","vs":"hello"}]}]`))
		if !errors.Is(err, sensors.ErrEmptyEntries) {
			t.Errorf("expected ErrEmptyEntries, got %v", err)
		}
	})
}

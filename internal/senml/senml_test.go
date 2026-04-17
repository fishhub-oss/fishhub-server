package senml_test

import (
	"errors"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/senml"
)

func TestParse(t *testing.T) {
	valid := `[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"temperature","u":"Cel","v":23.4}]}]`

	t.Run("valid payload", func(t *testing.T) {
		r, err := senml.Parse([]byte(valid))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if r.Temperature != 23.4 {
			t.Errorf("expected 23.4, got %v", r.Temperature)
		}
		if r.BaseTime != 1713000000 {
			t.Errorf("expected bt 1713000000, got %v", r.BaseTime)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		_, err := senml.Parse([]byte(`not json`))
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty array", func(t *testing.T) {
		_, err := senml.Parse([]byte(`[]`))
		if !errors.Is(err, senml.ErrEmptyPayload) {
			t.Errorf("expected ErrEmptyPayload, got %v", err)
		}
	})

	t.Run("missing base time", func(t *testing.T) {
		_, err := senml.Parse([]byte(`[{"bn":"fishhub/device/","e":[{"n":"temperature","u":"Cel","v":23.4}]}]`))
		if !errors.Is(err, senml.ErrMissingBaseTime) {
			t.Errorf("expected ErrMissingBaseTime, got %v", err)
		}
	})

	t.Run("missing temperature entry", func(t *testing.T) {
		_, err := senml.Parse([]byte(`[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"humidity","u":"%RH","v":55}]}]`))
		if !errors.Is(err, senml.ErrMissingTemperature) {
			t.Errorf("expected ErrMissingTemperature, got %v", err)
		}
	})

	t.Run("empty entries", func(t *testing.T) {
		_, err := senml.Parse([]byte(`[{"bn":"fishhub/device/","bt":1713000000,"e":[]}]`))
		if !errors.Is(err, senml.ErrMissingTemperature) {
			t.Errorf("expected ErrMissingTemperature, got %v", err)
		}
	})
}

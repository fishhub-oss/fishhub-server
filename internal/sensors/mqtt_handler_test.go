package sensors_test

import (
	"context"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
)

const validReadingPayload = `[{"bn":"fishhub/device/","bt":1713000000},{"n":"temperature","u":"Cel","v":23.4}]`

func newReadingsMQTTHandler(store *stubDeviceStore, writer *stubReadingWriter) *sensors.ReadingsMQTTHandler {
	svc := sensors.NewReadingsService(store, nil, writer, discardLogger)
	return sensors.NewReadingsMQTTHandler(store, svc, discardLogger)
}

func TestReadingsMQTTHandler_Handle(t *testing.T) {
	device := sensors.Device{ID: "dev-1", UserID: "user-1"}

	t.Run("valid topic and payload writes reading", func(t *testing.T) {
		store := &stubDeviceStore{findByIDDevice: device}
		writer := &stubReadingWriter{}
		h := newReadingsMQTTHandler(store, writer)

		h.Handle(context.Background(), "fishhub/dev-1/readings", []byte(validReadingPayload))

		if !writer.called {
			t.Error("expected writer to be called")
		}
		if writer.reading.DeviceID != "dev-1" {
			t.Errorf("expected device_id 'dev-1', got %q", writer.reading.DeviceID)
		}
		if writer.reading.UserID != "user-1" {
			t.Errorf("expected user_id 'user-1', got %q", writer.reading.UserID)
		}
	})

	t.Run("device not found does not write", func(t *testing.T) {
		store := &stubDeviceStore{findByIDErr: sensors.ErrDeviceNotFound}
		writer := &stubReadingWriter{}
		h := newReadingsMQTTHandler(store, writer)

		h.Handle(context.Background(), "fishhub/dev-x/readings", []byte(validReadingPayload))

		if writer.called {
			t.Error("expected writer not to be called")
		}
	})

	t.Run("malformed topic does not write", func(t *testing.T) {
		store := &stubDeviceStore{findByIDDevice: device}
		writer := &stubReadingWriter{}
		h := newReadingsMQTTHandler(store, writer)

		for _, topic := range []string{
			"fishhub/readings",
			"fishhub/dev-1/readings/extra",
			"other/dev-1/readings",
			"fishhub//readings",
		} {
			writer.called = false
			h.Handle(context.Background(), topic, []byte(validReadingPayload))
			if writer.called {
				t.Errorf("topic %q: expected writer not to be called", topic)
			}
		}
	})

	t.Run("malformed SenML payload does not panic", func(t *testing.T) {
		store := &stubDeviceStore{findByIDDevice: device}
		writer := &stubReadingWriter{}
		h := newReadingsMQTTHandler(store, writer)

		h.Handle(context.Background(), "fishhub/dev-1/readings", []byte("not json"))

		if writer.called {
			t.Error("expected writer not to be called on bad payload")
		}
	})

	t.Run("store error does not panic", func(t *testing.T) {
		store := &stubDeviceStore{findByIDErr: errSentinel}
		writer := &stubReadingWriter{}
		h := newReadingsMQTTHandler(store, writer)

		h.Handle(context.Background(), "fishhub/dev-1/readings", []byte(validReadingPayload))

		if writer.called {
			t.Error("expected writer not to be called on store error")
		}
	})
}

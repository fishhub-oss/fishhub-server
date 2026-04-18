package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/handler"
	"github.com/fishhub-oss/fishhub-server/internal/influx"
	"github.com/fishhub-oss/fishhub-server/internal/store"
)

// stubWriter captures the last WriteReading call.
type stubWriter struct {
	called  bool
	reading influx.Reading
	err     error
}

func (s *stubWriter) WriteReading(_ context.Context, r influx.Reading) error {
	s.called = true
	s.reading = r
	return s.err
}

type deviceStoreStub struct{ info store.DeviceInfo }

func (s *deviceStoreStub) LookupByToken(_ context.Context, _ string) (store.DeviceInfo, error) {
	return s.info, nil
}

type contextKey string

func withDevice(r *http.Request, info store.DeviceInfo) *http.Request {
	var enriched *http.Request
	called := false
	mw := auth.Authenticator(&deviceStoreStub{info: info})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enriched = r
		called = true
	})
	r.Header.Set("Authorization", "Bearer anytoken")
	w := httptest.NewRecorder()
	mw(inner).ServeHTTP(w, r)
	if !called {
		panic("middleware did not call next")
	}
	return enriched
}

const validSenML = `[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"temperature","u":"Cel","v":23.4}]}]`
const multiSenML = `[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"temperature","u":"Cel","v":23.4},{"n":"ph","u":"pH","v":7.2}]}]`

func TestReadingsHandler_Create(t *testing.T) {
	device := store.DeviceInfo{DeviceID: "device-uuid", UserID: "user-uuid"}

	t.Run("valid payload with writer returns 201 and calls writer", func(t *testing.T) {
		w := &stubWriter{}
		h := &handler.ReadingsHandler{Writer: w}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d", rec.Code)
		}
		if !w.called {
			t.Error("expected writer to be called")
		}
		if w.reading.DeviceID != "device-uuid" {
			t.Errorf("expected device_id 'device-uuid', got %q", w.reading.DeviceID)
		}
		if w.reading.UserID != "user-uuid" {
			t.Errorf("expected user_id 'user-uuid', got %q", w.reading.UserID)
		}
		if v, ok := w.reading.Measurements["temperature"].(float64); !ok || v != 23.4 {
			t.Errorf("expected temperature 23.4, got %v", w.reading.Measurements["temperature"])
		}
	})

	t.Run("multi-sensor payload writes all fields", func(t *testing.T) {
		w := &stubWriter{}
		h := &handler.ReadingsHandler{Writer: w}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(multiSenML)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d", rec.Code)
		}
		if len(w.reading.Measurements) != 2 {
			t.Errorf("expected 2 measurements, got %d", len(w.reading.Measurements))
		}
	})

	t.Run("writer error returns 500", func(t *testing.T) {
		w := &stubWriter{err: errors.New("influx down")}
		h := &handler.ReadingsHandler{Writer: w}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("nil writer returns 201 (degraded mode)", func(t *testing.T) {
		h := &handler.ReadingsHandler{Writer: nil}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d", rec.Code)
		}
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		h := &handler.ReadingsHandler{}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(`not json`)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("missing base time returns 400", func(t *testing.T) {
		h := &handler.ReadingsHandler{}
		body := `[{"bn":"fishhub/device/","e":[{"n":"temperature","v":23.4}]}]`
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(body)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("no device in context returns 401", func(t *testing.T) {
		h := &handler.ReadingsHandler{}
		req := httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})
}

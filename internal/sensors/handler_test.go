package sensors_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/platform"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/go-chi/chi/v5"
)

type stubTokenStore struct {
	result sensors.TokenResult
	err    error
}

func (s *stubTokenStore) CreateToken(_ context.Context, userID string) (sensors.TokenResult, error) {
	return s.result, s.err
}

func TestTokensHandler_Create_success(t *testing.T) {
	h := &sensors.TokensHandler{
		Store: &stubTokenStore{result: sensors.TokenResult{
			Token:    "abc123",
			DeviceID: "device-uuid",
			UserID:   "user-uuid",
		}},
		UserID: "user-uuid",
	}

	req := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	w := httptest.NewRecorder()
	h.Create(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}

	var body sensors.TokenResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Token != "abc123" {
		t.Errorf("unexpected token: %s", body.Token)
	}
	if body.DeviceID != "device-uuid" {
		t.Errorf("unexpected device_id: %s", body.DeviceID)
	}
	if body.UserID != "user-uuid" {
		t.Errorf("unexpected user_id: %s", body.UserID)
	}
}

func TestTokensHandler_Create_storeError(t *testing.T) {
	h := &sensors.TokensHandler{
		Store:  &stubTokenStore{err: errors.New("db down")},
		UserID: "user-uuid",
	}

	req := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

type stubReadingWriter struct {
	called  bool
	reading sensors.Reading
	err     error
}

func (s *stubReadingWriter) WriteReading(_ context.Context, r sensors.Reading) error {
	s.called = true
	s.reading = r
	return s.err
}

type stubDeviceStore struct {
	info         sensors.DeviceInfo
	device       sensors.Device
	findErr      error
}

func (s *stubDeviceStore) ListByUserID(_ context.Context, _ string) ([]sensors.Device, error) {
	return nil, nil
}

func (s *stubDeviceStore) LookupByToken(_ context.Context, _ string) (sensors.DeviceInfo, error) {
	return s.info, nil
}

func (s *stubDeviceStore) FindByIDAndUserID(_ context.Context, _, _ string) (sensors.Device, error) {
	return s.device, s.findErr
}

type stubReadingQuerier struct {
	points []sensors.ReadingPoint
	err    error
}

func (s *stubReadingQuerier) QueryReadings(_ context.Context, _ sensors.ReadingQuery) ([]sensors.ReadingPoint, error) {
	return s.points, s.err
}

func withDevice(r *http.Request, info sensors.DeviceInfo) *http.Request {
	var enriched *http.Request
	called := false
	mw := platform.DeviceAuthenticator(&stubDeviceStore{info: info})
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
	device := sensors.DeviceInfo{DeviceID: "device-uuid", UserID: "user-uuid"}

	t.Run("valid payload with writer returns 201 and calls writer", func(t *testing.T) {
		w := &stubReadingWriter{}
		h := &sensors.ReadingsHandler{Writer: w}
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
		w := &stubReadingWriter{}
		h := &sensors.ReadingsHandler{Writer: w}
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
		w := &stubReadingWriter{err: errors.New("influx down")}
		h := &sensors.ReadingsHandler{Writer: w}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("nil writer returns 201 (degraded mode)", func(t *testing.T) {
		h := &sensors.ReadingsHandler{Writer: nil}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d", rec.Code)
		}
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		h := &sensors.ReadingsHandler{}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(`not json`)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("missing base time returns 400", func(t *testing.T) {
		h := &sensors.ReadingsHandler{}
		body := `[{"bn":"fishhub/device/","e":[{"n":"temperature","v":23.4}]}]`
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(body)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("no device in context returns 401", func(t *testing.T) {
		h := &sensors.ReadingsHandler{}
		req := httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})
}

func withClaims(r *http.Request, userID string) *http.Request {
	ctx := auth.ContextWithClaims(r.Context(), auth.Claims{UserID: userID})
	return r.WithContext(ctx)
}

func withChiParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestReadingsQueryHandler_List(t *testing.T) {
	ts := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	points := []sensors.ReadingPoint{{Timestamp: ts, Temperature: 25.4}}

	makeReq := func(deviceID, query string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/readings"+query, nil)
		req = withClaims(req, "user-uuid")
		req = withChiParam(req, "id", deviceID)
		return req
	}

	t.Run("valid request returns 200 with readings", func(t *testing.T) {
		h := &sensors.ReadingsQueryHandler{
			Querier: &stubReadingQuerier{points: points},
			Devices: &stubDeviceStore{device: sensors.Device{ID: "dev-1"}},
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", "?from=2026-04-20T00:00:00Z&to=2026-04-21T00:00:00Z"))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body sensors.ReadingsQueryResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.DeviceID != "dev-1" {
			t.Errorf("expected device_id dev-1, got %s", body.DeviceID)
		}
		if len(body.Readings) != 1 {
			t.Fatalf("expected 1 reading, got %d", len(body.Readings))
		}
		if body.Readings[0].Temperature != 25.4 {
			t.Errorf("expected temperature 25.4, got %f", body.Readings[0].Temperature)
		}
	})

	t.Run("device not owned by user returns 404", func(t *testing.T) {
		h := &sensors.ReadingsQueryHandler{
			Querier: &stubReadingQuerier{},
			Devices: &stubDeviceStore{findErr: sensors.ErrDeviceNotFound},
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-other", ""))
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("invalid from param returns 400", func(t *testing.T) {
		h := &sensors.ReadingsQueryHandler{
			Querier: &stubReadingQuerier{},
			Devices: &stubDeviceStore{device: sensors.Device{ID: "dev-1"}},
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", "?from=not-a-date"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("empty readings returns 200 with empty array", func(t *testing.T) {
		h := &sensors.ReadingsQueryHandler{
			Querier: &stubReadingQuerier{points: []sensors.ReadingPoint{}},
			Devices: &stubDeviceStore{device: sensors.Device{ID: "dev-1"}},
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body sensors.ReadingsQueryResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Readings == nil || len(body.Readings) != 0 {
			t.Errorf("expected empty readings slice, got %v", body.Readings)
		}
	})

	t.Run("default params applied when omitted", func(t *testing.T) {
		q := &stubReadingQuerier{points: points}
		h := &sensors.ReadingsQueryHandler{
			Querier: q,
			Devices: &stubDeviceStore{device: sensors.Device{ID: "dev-1"}},
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body sensors.ReadingsQueryResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.From == "" || body.To == "" {
			t.Error("expected from/to to be set to defaults")
		}
	})
}

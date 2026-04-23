package sensors_test

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/go-chi/chi/v5"
)


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
	info        sensors.DeviceInfo
	device      sensors.Device
	findErr     error
	patchDevice sensors.Device
	patchErr    error
}

func (s *stubDeviceStore) ListByUserID(_ context.Context, _, _ string) ([]sensors.Device, error) {
	return nil, nil
}


func (s *stubDeviceStore) FindByIDAndUserID(_ context.Context, _, _ string) (sensors.Device, error) {
	return s.device, s.findErr
}

func (s *stubDeviceStore) PatchDevice(_ context.Context, _, _, _ string) (sensors.Device, error) {
	return s.patchDevice, s.patchErr
}

type stubReadingQuerier struct {
	points []sensors.ReadingPoint
	err    error
}

func (s *stubReadingQuerier) QueryReadings(_ context.Context, _ sensors.ReadingQuery) ([]sensors.ReadingPoint, error) {
	return s.points, s.err
}

func withDevice(r *http.Request, info sensors.DeviceInfo) *http.Request {
	ctx := context.WithValue(r.Context(), sensors.DeviceContextKey, info)
	return r.WithContext(ctx)
}

const validSenML = `[{"bn":"fishhub/device/","bt":1713000000},{"n":"temperature","u":"Cel","v":23.4}]`
const multiSenML = `[{"bn":"fishhub/device/","bt":1713000000},{"n":"temperature","u":"Cel","v":23.4},{"n":"ph","u":"pH","v":7.2}]`

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
		body := `[{"bn":"fishhub/device/"},{"n":"temperature","v":23.4}]`
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

// --- PatchDeviceHandler ---

func TestPatchDeviceHandler(t *testing.T) {
	ts := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	makeReq := func(deviceID, body string) *http.Request {
		req := httptest.NewRequest(http.MethodPatch, "/api/devices/"+deviceID, strings.NewReader(body))
		req = withClaims(req, "user-uuid")
		req = withChiParam(req, "id", deviceID)
		return req
	}

	t.Run("valid name returns 200 with updated device", func(t *testing.T) {
		updated := sensors.Device{ID: "dev-1", Name: "Tank A", CreatedAt: ts}
		h := &sensors.PatchDeviceHandler{Store: &stubDeviceStore{patchDevice: updated}}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", `{"name":"Tank A"}`))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body sensors.DeviceResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Name != "Tank A" {
			t.Errorf("expected name 'Tank A', got %q", body.Name)
		}
		if body.ID != "dev-1" {
			t.Errorf("expected id 'dev-1', got %q", body.ID)
		}
	})

	t.Run("empty name returns 400", func(t *testing.T) {
		h := &sensors.PatchDeviceHandler{Store: &stubDeviceStore{}}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", `{"name":""}`))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("device not found returns 404", func(t *testing.T) {
		h := &sensors.PatchDeviceHandler{Store: &stubDeviceStore{patchErr: sensors.ErrDeviceNotFound}}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-x", `{"name":"Tank A"}`))
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("store error returns 500", func(t *testing.T) {
		h := &sensors.PatchDeviceHandler{Store: &stubDeviceStore{patchErr: errors.New("db down")}}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", `{"name":"Tank A"}`))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("missing claims returns 401", func(t *testing.T) {
		h := &sensors.PatchDeviceHandler{Store: &stubDeviceStore{}}
		req := httptest.NewRequest(http.MethodPatch, "/api/devices/dev-1", strings.NewReader(`{"name":"Tank A"}`))
		req = withChiParam(req, "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})
}

// --- ProvisionHandler ---

type stubProvisioningStore struct {
	deviceID string
	code     string
	getErr   error

	claimedDeviceID string
	claimErr        error

	activateErr error
}

func (s *stubProvisioningStore) GetOrCreatePending(_ context.Context, _ string) (string, string, error) {
	return s.deviceID, s.code, s.getErr
}

func (s *stubProvisioningStore) ClaimCode(_ context.Context, _ string) (string, string, error) {
	return s.claimedDeviceID, "user-uuid", s.claimErr
}

func (s *stubProvisioningStore) Activate(_ context.Context, _ string) error {
	return s.activateErr
}

func TestProvisionHandler(t *testing.T) {
	t.Run("returns 201 with code and device_id", func(t *testing.T) {
		h := &sensors.ProvisionHandler{
			Store: &stubProvisioningStore{deviceID: "dev-uuid", code: "ABC123"},
		}
		req := withClaims(httptest.NewRequest(http.MethodPost, "/api/devices/provision", nil), "user-uuid")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rec.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["code"] != "ABC123" {
			t.Errorf("expected code ABC123, got %s", body["code"])
		}
		if body["device_id"] != "dev-uuid" {
			t.Errorf("expected device_id dev-uuid, got %s", body["device_id"])
		}
	})

	t.Run("missing claims returns 401", func(t *testing.T) {
		h := &sensors.ProvisionHandler{Store: &stubProvisioningStore{}}
		req := httptest.NewRequest(http.MethodPost, "/api/devices/provision", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("store error returns 500", func(t *testing.T) {
		h := &sensors.ProvisionHandler{
			Store: &stubProvisioningStore{getErr: errors.New("db down")},
		}
		req := withClaims(httptest.NewRequest(http.MethodPost, "/api/devices/provision", nil), "user-uuid")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})
}

// stubSigner implements devicejwt.Signer for tests.
type stubSigner struct {
	token string
	err   error
}

func (s *stubSigner) Sign(_, _ string) (string, error)       { return s.token, s.err }
func (s *stubSigner) PublicKey() *rsa.PublicKey              { return nil }
func (s *stubSigner) KID() string                            { return "" }
func (s *stubSigner) Issuer() string                         { return "" }

// --- ActivateHandler ---

func TestActivateHandler(t *testing.T) {
	validBody := `{"code":"ABC123"}`

	t.Run("returns 201 with jwt token and device_id", func(t *testing.T) {
		h := &sensors.ActivateHandler{
			Store:  &stubProvisioningStore{claimedDeviceID: "dev-uuid"},
			Signer: &stubSigner{token: "signed.jwt.token"},
		}
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rec.Code)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["device_id"] != "dev-uuid" {
			t.Errorf("expected device_id dev-uuid, got %s", body["device_id"])
		}
		if body["token"] != "signed.jwt.token" {
			t.Errorf("expected token=signed.jwt.token, got %q", body["token"])
		}
	})

	t.Run("signer error returns 500", func(t *testing.T) {
		h := &sensors.ActivateHandler{
			Store:  &stubProvisioningStore{claimedDeviceID: "dev-uuid"},
			Signer: &stubSigner{err: errors.New("sign failed")},
		}
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("missing code returns 400", func(t *testing.T) {
		h := &sensors.ActivateHandler{Store: &stubProvisioningStore{}}
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("unknown code returns 404", func(t *testing.T) {
		h := &sensors.ActivateHandler{
			Store: &stubProvisioningStore{claimErr: sensors.ErrCodeNotFound},
		}
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("already used code returns 409", func(t *testing.T) {
		h := &sensors.ActivateHandler{
			Store: &stubProvisioningStore{claimErr: sensors.ErrCodeAlreadyUsed},
		}
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Errorf("expected 409, got %d", rec.Code)
		}
	})

	t.Run("activate error returns 500", func(t *testing.T) {
		h := &sensors.ActivateHandler{
			Store: &stubProvisioningStore{
				claimedDeviceID: "dev-uuid",
				activateErr:     errors.New("db down"),
			},
		}
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})
}

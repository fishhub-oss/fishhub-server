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
	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

const validSenML = `[{"bn":"fishhub/device/","bt":1713000000},{"n":"temperature","u":"Cel","v":23.4}]`
const multiSenML = `[{"bn":"fishhub/device/","bt":1713000000},{"n":"temperature","u":"Cel","v":23.4},{"n":"ph","u":"pH","v":7.2}]`

func withDevice(r *http.Request, info sensors.DeviceInfo) *http.Request {
	ctx := context.WithValue(r.Context(), sensors.DeviceContextKey, info)
	return r.WithContext(ctx)
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

func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func newReadingsService(writer *stubReadingWriter, querier *stubReadingQuerier, store *stubDeviceStore) *sensors.ReadingsService {
	var w sensors.ReadingWriter
	if writer != nil {
		w = writer
	}
	return sensors.NewReadingsService(store, querier, w, discardLogger)
}

// ── ReadingsHandler ───────────────────────────────────────────────────────────

func TestReadingsHandler_Create(t *testing.T) {
	device := sensors.DeviceInfo{DeviceID: "device-uuid", UserID: "user-uuid"}

	t.Run("valid payload with writer returns 201 and calls writer", func(t *testing.T) {
		w := &stubReadingWriter{}
		h := &sensors.ReadingsHandler{Service: newReadingsService(w, nil, &stubDeviceStore{})}
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
		h := &sensors.ReadingsHandler{Service: newReadingsService(w, nil, &stubDeviceStore{})}
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
		h := &sensors.ReadingsHandler{Service: newReadingsService(w, nil, &stubDeviceStore{})}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("nil writer returns 201 (degraded mode)", func(t *testing.T) {
		h := &sensors.ReadingsHandler{Service: newReadingsService(nil, nil, &stubDeviceStore{})}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d", rec.Code)
		}
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		h := &sensors.ReadingsHandler{Service: newReadingsService(nil, nil, &stubDeviceStore{})}
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(`not json`)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("missing base time returns 400", func(t *testing.T) {
		h := &sensors.ReadingsHandler{Service: newReadingsService(nil, nil, &stubDeviceStore{})}
		body := `[{"bn":"fishhub/device/"},{"n":"temperature","v":23.4}]`
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(body)), device)
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("no device in context returns 401", func(t *testing.T) {
		h := &sensors.ReadingsHandler{Service: newReadingsService(nil, nil, &stubDeviceStore{})}
		req := httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})
}

// ── ReadingsQueryHandler ──────────────────────────────────────────────────────

func TestReadingsQueryHandler_List(t *testing.T) {
	ts := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	points := []sensors.ReadingPoint{{Timestamp: ts, Values: map[string]float64{"temperature": 25.4}}}

	makeReq := func(deviceID, query string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/readings"+query, nil)
		req = withClaims(req, "user-uuid")
		req = withChiParam(req, "id", deviceID)
		return req
	}

	t.Run("valid request returns 200 with readings", func(t *testing.T) {
		h := &sensors.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{points: points}, &stubDeviceStore{device: newDevice("dev-1")}),
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
		if body.Readings[0].Values["temperature"] != 25.4 {
			t.Errorf("expected temperature 25.4, got %f", body.Readings[0].Values["temperature"])
		}
	})

	t.Run("device not owned by user returns 404", func(t *testing.T) {
		h := &sensors.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{}, &stubDeviceStore{findErr: sensors.ErrDeviceNotFound}),
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-other", ""))
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("invalid from param returns 400", func(t *testing.T) {
		h := &sensors.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{}, &stubDeviceStore{device: newDevice("dev-1")}),
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", "?from=not-a-date"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("empty readings returns 200 with empty array", func(t *testing.T) {
		h := &sensors.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{points: []sensors.ReadingPoint{}}, &stubDeviceStore{device: newDevice("dev-1")}),
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
		h := &sensors.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{points: points}, &stubDeviceStore{device: newDevice("dev-1")}),
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

// ── DevicesHandler ────────────────────────────────────────────────────────────

func TestDevicesHandler_List(t *testing.T) {
	newSvc := func(store *stubDeviceStore) *sensors.DeviceService {
		return sensors.NewDeviceService(store, &stubHiveMQClient{}, &stubPublisher{}, discardLogger)
	}

	t.Run("returns devices for user", func(t *testing.T) {
		devices := []sensors.Device{newDevice("dev-1"), newDevice("dev-2")}
		h := &sensors.DevicesHandler{Service: newSvc(&stubDeviceStore{listDevices: devices})}
		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/devices", nil), "usr-1")
		rec := httptest.NewRecorder()
		h.List(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body []sensors.DeviceResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body) != 2 {
			t.Errorf("expected 2 devices, got %d", len(body))
		}
	})

	t.Run("missing claims returns 401", func(t *testing.T) {
		h := &sensors.DevicesHandler{Service: newSvc(&stubDeviceStore{})}
		rec := httptest.NewRecorder()
		h.List(rec, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("store error returns 500", func(t *testing.T) {
		h := &sensors.DevicesHandler{Service: newSvc(&stubDeviceStore{listErr: errors.New("db down")})}
		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/devices", nil), "usr-1")
		rec := httptest.NewRecorder()
		h.List(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})
}

// ── PatchDeviceHandler ────────────────────────────────────────────────────────

func TestPatchDeviceHandler(t *testing.T) {
	ts := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	makeReq := func(deviceID, body string) *http.Request {
		req := httptest.NewRequest(http.MethodPatch, "/api/devices/"+deviceID, strings.NewReader(body))
		req = withClaims(req, "user-uuid")
		req = withChiParam(req, "id", deviceID)
		return req
	}

	newPatchSvc := func(store *stubDeviceStore) *sensors.DeviceService {
		return sensors.NewDeviceService(store, &stubHiveMQClient{}, &stubPublisher{}, discardLogger)
	}

	t.Run("valid name returns 200 with updated device", func(t *testing.T) {
		updated := sensors.Device{ID: "dev-1", Name: "Tank A", CreatedAt: ts}
		h := &sensors.PatchDeviceHandler{Service: newPatchSvc(&stubDeviceStore{patchDevice: updated})}
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
		h := &sensors.PatchDeviceHandler{Service: newPatchSvc(&stubDeviceStore{})}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", `{"name":""}`))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("device not found returns 404", func(t *testing.T) {
		h := &sensors.PatchDeviceHandler{Service: newPatchSvc(&stubDeviceStore{patchErr: sensors.ErrDeviceNotFound})}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-x", `{"name":"Tank A"}`))
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("store error returns 500", func(t *testing.T) {
		h := &sensors.PatchDeviceHandler{Service: newPatchSvc(&stubDeviceStore{patchErr: errors.New("db down")})}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", `{"name":"Tank A"}`))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("missing claims returns 401", func(t *testing.T) {
		h := &sensors.PatchDeviceHandler{Service: newPatchSvc(&stubDeviceStore{})}
		req := httptest.NewRequest(http.MethodPatch, "/api/devices/dev-1", strings.NewReader(`{"name":"Tank A"}`))
		req = withChiParam(req, "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})
}

// ── ProvisionHandler ──────────────────────────────────────────────────────────

func TestProvisionHandler(t *testing.T) {
	newProvSvc := func(store *stubProvisioningStore) *sensors.ProvisioningService {
		return sensors.NewProvisioningService(store, discardLogger)
	}

	t.Run("returns 201 with code", func(t *testing.T) {
		h := &sensors.ProvisionHandler{
			Service: newProvSvc(&stubProvisioningStore{code: "ABC123"}),
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
		if _, ok := body["device_id"]; ok {
			t.Error("device_id should not be present in provision response")
		}
	})

	t.Run("missing claims returns 401", func(t *testing.T) {
		h := &sensors.ProvisionHandler{Service: newProvSvc(&stubProvisioningStore{})}
		req := httptest.NewRequest(http.MethodPost, "/api/devices/provision", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("store error returns 500", func(t *testing.T) {
		h := &sensors.ProvisionHandler{
			Service: newProvSvc(&stubProvisioningStore{getErr: errors.New("db down")}),
		}
		req := withClaims(httptest.NewRequest(http.MethodPost, "/api/devices/provision", nil), "user-uuid")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})
}

// ── ActivateHandler ───────────────────────────────────────────────────────────

func newActivateHandler(t *testing.T, store *stubProvisioningStore, signer *stubSigner) *sensors.ActivateHandler {
	t.Helper()
	db := testutil.NewTestDB(t)
	return &sensors.ActivateHandler{
		Service: sensors.NewActivationService(db, store, &stubOutboxStore{}, signer, "broker.example.com", 8883, discardLogger),
	}
}

func TestActivateHandler(t *testing.T) {
	validBody := `{"code":"ABC123"}`

	t.Run("returns 201 with token, device_id and mqtt credentials", func(t *testing.T) {
		h := newActivateHandler(t,
			&stubProvisioningStore{claimedDeviceID: "dev-uuid"},
			&stubSigner{token: "signed.jwt.token"},
		)
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rec.Code)
		}
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["device_id"] != "dev-uuid" {
			t.Errorf("expected device_id dev-uuid, got %v", body["device_id"])
		}
		if body["token"] != "signed.jwt.token" {
			t.Errorf("expected token=signed.jwt.token, got %v", body["token"])
		}
		if body["mqtt_username"] != "dev-uuid" {
			t.Errorf("expected mqtt_username=dev-uuid, got %v", body["mqtt_username"])
		}
		if body["mqtt_password"] == "" {
			t.Error("expected non-empty mqtt_password")
		}
		if body["mqtt_host"] != "broker.example.com" {
			t.Errorf("expected mqtt_host=broker.example.com, got %v", body["mqtt_host"])
		}
		if body["mqtt_port"] != float64(8883) {
			t.Errorf("expected mqtt_port=8883, got %v", body["mqtt_port"])
		}
	})

	t.Run("signer error returns 500", func(t *testing.T) {
		h := newActivateHandler(t,
			&stubProvisioningStore{claimedDeviceID: "dev-uuid"},
			&stubSigner{err: errors.New("sign failed")},
		)
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("missing code returns 400", func(t *testing.T) {
		h := newActivateHandler(t, &stubProvisioningStore{}, &stubSigner{})
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("unknown code returns 404", func(t *testing.T) {
		h := newActivateHandler(t,
			&stubProvisioningStore{claimErr: sensors.ErrCodeNotFound},
			&stubSigner{},
		)
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("already used code returns 409", func(t *testing.T) {
		h := newActivateHandler(t,
			&stubProvisioningStore{claimErr: sensors.ErrCodeAlreadyUsed},
			&stubSigner{},
		)
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Errorf("expected 409, got %d", rec.Code)
		}
	})

	t.Run("activate error returns 500", func(t *testing.T) {
		h := newActivateHandler(t,
			&stubProvisioningStore{claimedDeviceID: "dev-uuid", activateErr: errors.New("db down")},
			&stubSigner{},
		)
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})
}

// ── CommandHandler ────────────────────────────────────────────────────────────

func newCommandHandler(store *stubDeviceStore, pub *stubPublisher) *sensors.CommandHandler {
	return &sensors.CommandHandler{
		Service: sensors.NewDeviceService(store, &stubHiveMQClient{}, pub, discardLogger),
	}
}

func TestCommandHandler(t *testing.T) {
	const body = `{"action":"set","state":true,"id":"cmd-1"}`

	makeReq := func(body, userID string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r = withClaims(r, userID)
		r = withChiParams(r, map[string]string{"id": "dev-1", "name": "light"})
		return r
	}

	t.Run("204 on success", func(t *testing.T) {
		pub := &stubPublisher{}
		h := newCommandHandler(&stubDeviceStore{}, pub)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq(body, "user-1"))
		if rec.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rec.Code)
		}
		if !pub.called {
			t.Error("expected publisher to be called")
		}
	})

	t.Run("404 when device not found", func(t *testing.T) {
		h := newCommandHandler(&stubDeviceStore{findErr: sensors.ErrDeviceNotFound}, &stubPublisher{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq(body, "user-1"))
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("400 on invalid action", func(t *testing.T) {
		h := newCommandHandler(&stubDeviceStore{}, &stubPublisher{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq(`{"action":"invalid"}`, "user-1"))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("500 on publisher error", func(t *testing.T) {
		h := newCommandHandler(&stubDeviceStore{}, &stubPublisher{err: errors.New("broker down")})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq(body, "user-1"))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})
}

// ── DeleteDeviceHandler ───────────────────────────────────────────────────────

func newDeleteHandler(store *stubDeviceStore, mq *stubHiveMQClient) *sensors.DeleteDeviceHandler {
	return &sensors.DeleteDeviceHandler{
		Service: sensors.NewDeviceService(store, mq, &stubPublisher{}, discardLogger),
	}
}

func TestDeleteDeviceHandler(t *testing.T) {
	makeReq := func(deviceID, userID string) *http.Request {
		req := httptest.NewRequest(http.MethodDelete, "/api/devices/"+deviceID, nil)
		if userID != "" {
			req = withClaims(req, userID)
		}
		req = withChiParam(req, "id", deviceID)
		return req
	}

	t.Run("204 when device deleted and HiveMQ succeeds", func(t *testing.T) {
		h := newDeleteHandler(&stubDeviceStore{deleteMQTTUser: "dev-uuid"}, &stubHiveMQClient{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-uuid", "user-uuid"))
		if rec.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rec.Code)
		}
	})

	t.Run("204 when device deleted but HiveMQ fails (non-fatal)", func(t *testing.T) {
		h := newDeleteHandler(&stubDeviceStore{deleteMQTTUser: "dev-uuid"}, &stubHiveMQClient{err: errors.New("hivemq down")})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-uuid", "user-uuid"))
		if rec.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rec.Code)
		}
	})

	t.Run("204 when device has no mqtt_username (HiveMQ skipped)", func(t *testing.T) {
		h := newDeleteHandler(&stubDeviceStore{deleteMQTTUser: ""}, &stubHiveMQClient{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-uuid", "user-uuid"))
		if rec.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rec.Code)
		}
	})

	t.Run("404 when device not found", func(t *testing.T) {
		h := newDeleteHandler(&stubDeviceStore{deleteErr: sensors.ErrDeviceNotFound}, &stubHiveMQClient{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-x", "user-uuid"))
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("500 on store error", func(t *testing.T) {
		h := newDeleteHandler(&stubDeviceStore{deleteErr: errors.New("db down")}, &stubHiveMQClient{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-uuid", "user-uuid"))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("401 when no claims", func(t *testing.T) {
		h := newDeleteHandler(&stubDeviceStore{}, &stubHiveMQClient{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-uuid", ""))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})
}

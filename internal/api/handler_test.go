package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/api"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func withDevice(r *http.Request, info device.Info) *http.Request {
	ctx := context.WithValue(r.Context(), device.ContextKey, info)
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

func newReadingsService(writer *stubReadingWriter, querier *stubReadingQuerier, finder *stubDeviceFinder) *measurement.ReadingsService {
	var w measurement.Writer
	if writer != nil {
		w = writer
	}
	return measurement.NewReadingsService(finder, querier, w, discardLogger)
}

// ── ReadingsQueryHandler ──────────────────────────────────────────────────────

func TestReadingsQueryHandler_List(t *testing.T) {
	ts := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	points := []measurement.Point{{Timestamp: ts, Values: map[string]any{"temperature": 25.4}}}

	makeReq := func(deviceID, query string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/devices/"+deviceID+"/readings"+query, nil)
		req = withClaims(req, "user-uuid")
		req = withChiParam(req, "id", deviceID)
		return req
	}

	t.Run("valid request returns 200 with readings", func(t *testing.T) {
		h := &api.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{points: points}, &stubDeviceFinder{}),
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", "?from=2026-04-20T00:00:00Z&to=2026-04-21T00:00:00Z"))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body api.ReadingsQueryResponse
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
			t.Errorf("expected temperature 25.4, got %v", body.Readings[0].Values["temperature"])
		}
	})

	t.Run("device not owned by user returns 404", func(t *testing.T) {
		h := &api.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{}, &stubDeviceFinder{err: device.ErrNotFound}),
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-other", ""))
		assertErrorCode(t, rec, http.StatusNotFound, "device_not_found")
	})

	t.Run("invalid from param returns 400", func(t *testing.T) {
		h := &api.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{}, &stubDeviceFinder{}),
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", "?from=not-a-date"))
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("empty readings returns 200 with empty array", func(t *testing.T) {
		h := &api.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{points: []measurement.Point{}}, &stubDeviceFinder{}),
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body api.ReadingsQueryResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Readings == nil || len(body.Readings) != 0 {
			t.Errorf("expected empty readings slice, got %v", body.Readings)
		}
	})

	t.Run("string values are included in response", func(t *testing.T) {
		stringPoints := []measurement.Point{{
			Timestamp: ts,
			Values:    map[string]any{"light/source": "schedule", "temperature": 25.4},
		}}
		h := &api.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{points: stringPoints}, &stubDeviceFinder{}),
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body api.ReadingsQueryResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body.Readings) != 1 {
			t.Fatalf("expected 1 reading, got %d", len(body.Readings))
		}
		if body.Readings[0].Values["light/source"] != "schedule" {
			t.Errorf("expected light/source 'schedule', got %v", body.Readings[0].Values["light/source"])
		}
	})

	t.Run("default params applied when omitted", func(t *testing.T) {
		h := &api.ReadingsQueryHandler{
			Service: newReadingsService(nil, &stubReadingQuerier{points: points}, &stubDeviceFinder{}),
		}
		rec := httptest.NewRecorder()
		h.List(rec, makeReq("dev-1", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body api.ReadingsQueryResponse
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
	newSvc := func(store *stubDeviceStore) *device.Service {
		return device.NewService(store, &stubHiveMQClient{}, &stubPublisher{}, discardLogger)
	}

	t.Run("returns devices for user", func(t *testing.T) {
		devices := []device.Device{newDevice("dev-1"), newDevice("dev-2")}
		h := &api.DevicesHandler{Service: newSvc(&stubDeviceStore{listDevices: devices})}
		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/devices", nil), "usr-1")
		rec := httptest.NewRecorder()
		h.List(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body []api.DeviceResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body) != 2 {
			t.Errorf("expected 2 devices, got %d", len(body))
		}
	})

	t.Run("store error returns 500", func(t *testing.T) {
		h := &api.DevicesHandler{Service: newSvc(&stubDeviceStore{listErr: errors.New("db down")})}
		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/devices", nil), "usr-1")
		rec := httptest.NewRecorder()
		h.List(rec, req)
		assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
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

	newPatchSvc := func(store *stubDeviceStore) *device.Service {
		return device.NewService(store, &stubHiveMQClient{}, &stubPublisher{}, discardLogger)
	}

	t.Run("valid name returns 200 with updated device", func(t *testing.T) {
		updated := device.Device{ID: "dev-1", Name: "Tank A", CreatedAt: ts}
		h := &api.PatchDeviceHandler{Service: newPatchSvc(&stubDeviceStore{patchDevice: updated})}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", `{"name":"Tank A"}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body api.DeviceResponse
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
		h := &api.PatchDeviceHandler{Service: newPatchSvc(&stubDeviceStore{})}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", `{"name":""}`))
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("device not found returns 404", func(t *testing.T) {
		h := &api.PatchDeviceHandler{Service: newPatchSvc(&stubDeviceStore{patchErr: device.ErrNotFound})}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-x", `{"name":"Tank A"}`))
		assertErrorCode(t, rec, http.StatusNotFound, "device_not_found")
	})

	t.Run("store error returns 500", func(t *testing.T) {
		h := &api.PatchDeviceHandler{Service: newPatchSvc(&stubDeviceStore{patchErr: errors.New("db down")})}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", `{"name":"Tank A"}`))
		assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	})

}

// ── ProvisionHandler ──────────────────────────────────────────────────────────

func TestProvisionHandler(t *testing.T) {
	newProvSvc := func(store *stubProvisioningStore) *provisioning.Service {
		return provisioning.NewService(store, discardLogger)
	}

	t.Run("returns 201 with code", func(t *testing.T) {
		h := &api.ProvisionHandler{
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

	t.Run("store error returns 500", func(t *testing.T) {
		h := &api.ProvisionHandler{
			Service: newProvSvc(&stubProvisioningStore{getErr: errors.New("db down")}),
		}
		req := withClaims(httptest.NewRequest(http.MethodPost, "/api/devices/provision", nil), "user-uuid")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	})
}

// ── ActivateHandler ───────────────────────────────────────────────────────────

type stubTimezoneReader struct{}

func (s *stubTimezoneReader) GetTimezone(_ context.Context, _ string) (string, error) {
	return "UTC", nil
}

func newActivateHandler(t *testing.T, store *stubProvisioningStore, signer *stubSigner) *api.ActivateHandler {
	t.Helper()
	db := testutil.NewTestDB(t)
	return &api.ActivateHandler{
		Service: provisioning.NewActivationService(db, store, &stubOutboxStore{}, signer, &stubTimezoneReader{}, &stubModelIDResolver{}, discardLogger),
	}
}

func TestActivateHandler(t *testing.T) {
	validBody := `{"code":"ABC123"}`

	t.Run("returns 202 with token and device_id", func(t *testing.T) {
		h := newActivateHandler(t,
			&stubProvisioningStore{claimedDeviceID: "dev-uuid"},
			&stubSigner{token: "signed.jwt.token"},
		)
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d", rec.Code)
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
		if _, ok := body["mqtt_username"]; ok {
			t.Error("mqtt_username should not be present in 202 response")
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
		assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	})

	t.Run("missing code returns 400", func(t *testing.T) {
		h := newActivateHandler(t, &stubProvisioningStore{}, &stubSigner{})
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("unknown code returns 404", func(t *testing.T) {
		h := newActivateHandler(t,
			&stubProvisioningStore{claimErr: provisioning.ErrCodeNotFound},
			&stubSigner{},
		)
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "provisioning_code_not_found")
	})

	t.Run("already used code returns 409", func(t *testing.T) {
		h := newActivateHandler(t,
			&stubProvisioningStore{claimErr: provisioning.ErrCodeAlreadyUsed},
			&stubSigner{},
		)
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusConflict, "provisioning_code_conflict")
	})

	t.Run("activate error returns 500", func(t *testing.T) {
		h := newActivateHandler(t,
			&stubProvisioningStore{claimedDeviceID: "dev-uuid", activateErr: errors.New("db down")},
			&stubSigner{},
		)
		req := httptest.NewRequest(http.MethodPost, "/devices/activate", strings.NewReader(validBody))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	})
}

// ── ActivationStatusHandler ──────────────────────────────────────────────────

func TestActivationStatusHandler(t *testing.T) {
	makeReq := func(deviceID string, info device.Info) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/devices/"+deviceID+"/status", nil)
		req = withDevice(req, info)
		req = withChiParam(req, "id", deviceID)
		return req
	}

	t.Run("provisioning returns 200 with status=provisioning", func(t *testing.T) {
		h := &api.ActivationStatusHandler{
			Store:    &stubDeviceStore{},
			MQTTHost: "mqtt.example.com",
			MQTTPort: 8883,
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", device.Info{DeviceID: "dev-1", UserID: "usr-1"}))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["status"] != "provisioning" {
			t.Errorf("expected status=provisioning, got %v", body["status"])
		}
		if _, ok := body["mqtt_username"]; ok {
			t.Error("mqtt_username should not be present when provisioning")
		}
	})

	t.Run("ready returns 200 with credentials", func(t *testing.T) {
		h := &api.ActivationStatusHandler{
			Store: &stubActivationStatusStore{
				status: device.ActivationStatus{
					Ready:        true,
					MQTTUsername: "device-abc",
					MQTTPassword: "secret-pass",
				},
			},
			MQTTHost: "mqtt.example.com",
			MQTTPort: 8883,
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-1", device.Info{DeviceID: "dev-1", UserID: "usr-1"}))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["status"] != "ready" {
			t.Errorf("expected status=ready, got %v", body["status"])
		}
		if body["mqtt_username"] != "device-abc" {
			t.Errorf("expected mqtt_username=device-abc, got %v", body["mqtt_username"])
		}
		if body["mqtt_host"] != "mqtt.example.com" {
			t.Errorf("expected mqtt_host=mqtt.example.com, got %v", body["mqtt_host"])
		}
	})

	t.Run("device not found returns 404", func(t *testing.T) {
		h := &api.ActivationStatusHandler{
			Store: &stubActivationStatusStore{err: device.ErrNotFound},
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-x", device.Info{DeviceID: "dev-x", UserID: "usr-1"}))
		assertErrorCode(t, rec, http.StatusNotFound, "device_not_found")
	})

	t.Run("device ID mismatch returns 403", func(t *testing.T) {
		h := &api.ActivationStatusHandler{Store: &stubDeviceStore{}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/devices/dev-2/status", nil)
		req = withDevice(req, device.Info{DeviceID: "dev-1", UserID: "usr-1"})
		req = withChiParam(req, "id", "dev-2")
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusForbidden, "forbidden")
	})

	t.Run("no device in context returns 401", func(t *testing.T) {
		h := &api.ActivationStatusHandler{Store: &stubDeviceStore{}}
		req := httptest.NewRequest(http.MethodGet, "/devices/dev-1/status", nil)
		req = withChiParam(req, "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	})
}

// ── CommandHandler ────────────────────────────────────────────────────────────

func newCommandHandler(t *testing.T, pStore *stubPeripheralStore, pub *stubPublisher) *api.CommandHandler {
	t.Helper()
	return &api.CommandHandler{
		Service: newPeripheralService(t, pStore, pub),
	}
}

func TestCommandHandler(t *testing.T) {
	const body = `{"command":"set","id":"cmd-1","value":1}`

	relay := peripheral.Peripheral{ID: "p-1", DeviceID: "dev-1", Kind: "relay", Pin: 5, Category: "actuator"}

	makeReq := func(body, userID string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r = withClaims(r, userID)
		r = withChiParams(r, map[string]string{"id": "dev-1", "peripheralId": "p-1"})
		return r
	}

	t.Run("204 on success", func(t *testing.T) {
		pub := &stubPublisher{}
		h := newCommandHandler(t, &stubPeripheralStore{created: relay}, pub)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq(body, "user-1"))
		if rec.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rec.Code)
		}
		if !pub.called {
			t.Error("expected publisher to be called")
		}
	})

	t.Run("404 when peripheral not found", func(t *testing.T) {
		h := newCommandHandler(t, &stubPeripheralStore{createErr: peripheral.ErrNotFound}, &stubPublisher{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq(body, "user-1"))
		assertErrorCode(t, rec, http.StatusNotFound, "peripheral_not_found")
	})

	t.Run("400 on invalid action", func(t *testing.T) {
		h := newCommandHandler(t, &stubPeripheralStore{created: relay}, &stubPublisher{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq(`{"command":"invalid"}`, "user-1"))
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("500 on publisher error", func(t *testing.T) {
		h := newCommandHandler(t, &stubPeripheralStore{created: relay}, &stubPublisher{err: errors.New("broker down")})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq(body, "user-1"))
		assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	})
}

// ── DeleteDeviceHandler ───────────────────────────────────────────────────────

func newDeleteHandler(store *stubDeviceStore, mq *stubHiveMQClient) *api.DeleteDeviceHandler {
	return &api.DeleteDeviceHandler{
		Service: device.NewService(store, mq, &stubPublisher{}, discardLogger),
	}
}

func TestDeleteDeviceHandler(t *testing.T) {
	makeReq := func(deviceID, userID string) *http.Request {
		req := httptest.NewRequest(http.MethodDelete, "/api/devices/"+deviceID, nil)
		req = withClaims(req, userID)
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
		h := newDeleteHandler(&stubDeviceStore{deleteErr: device.ErrNotFound}, &stubHiveMQClient{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-x", "user-uuid"))
		assertErrorCode(t, rec, http.StatusNotFound, "device_not_found")
	})

	t.Run("500 on store error", func(t *testing.T) {
		h := newDeleteHandler(&stubDeviceStore{deleteErr: errors.New("db down")}, &stubHiveMQClient{})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, makeReq("dev-uuid", "user-uuid"))
		assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	})

}

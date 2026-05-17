package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/api"
	"github.com/fishhub-oss/fishhub-server/internal/devicemodel"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

func newPeripheral(name string) peripheral.Peripheral {
	mode := "automatic"
	return peripheral.Peripheral{
		ID:          "pid-1",
		DeviceID:    "dev-1",
		Name:        name,
		Kind:        "relay",
		Pin:         5,
		Category:    "actuator",
		ControlMode: &mode,
		Schedule:    nil,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

func strPtr(s string) *string { return &s }

func newPeripheralService(t *testing.T, store *stubPeripheralStore, pub *stubPublisher) *peripheral.Service {
	t.Helper()
	return peripheral.NewService(testutil.NewTestDB(t), store, &stubOutboxStore{}, nil, pub, discardLogger)
}

// ── ListPeripheralsHandler ────────────────────────────────────────────────────

func TestListPeripheralsHandler(t *testing.T) {
	t.Run("returns 200 with peripheral list", func(t *testing.T) {
		store := &stubPeripheralStore{listed: []peripheral.Peripheral{newPeripheral("light")}}
		svc := peripheral.NewService(nil, store, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.ListPeripheralsHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			"id", "dev-1",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp []map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp) != 1 || resp[0]["name"] != "light" {
			t.Errorf("unexpected response: %v", resp)
		}
	})

	t.Run("unknown device returns 200 with empty list", func(t *testing.T) {
		store := &stubPeripheralStore{listed: []peripheral.Peripheral{}}
		svc := peripheral.NewService(nil, store, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.ListPeripheralsHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			"id", "unknown-device",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		var resp []any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp) != 0 {
			t.Errorf("expected empty list, got %v", resp)
		}
	})

}

// ── SetPeripheralScheduleHandler ─────────────────────────────────────────────

func TestSetPeripheralScheduleHandler(t *testing.T) {
	schedule := `[{"from":"08:00","to":"18:00","value":1.0}]`

	t.Run("returns 200 with updated peripheral", func(t *testing.T) {
		p := newPeripheral("light")
		p.Schedule = []peripheral.ScheduleWindow{{From: "08:00", To: "18:00", Value: 1.0}}
		store := &stubPeripheralStore{scheduled: p}
		svc := peripheral.NewService(nil, store, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.SetPeripheralScheduleHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPut, "/", strings.NewReader(schedule)), "user-1"),
			map[string]string{"id": "dev-1", "name": "light"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["name"] != "light" {
			t.Errorf("unexpected name: %v", resp["name"])
		}
	})

	t.Run("peripheral not found returns 404", func(t *testing.T) {
		store := &stubPeripheralStore{schedErr: peripheral.ErrNotFound}
		svc := peripheral.NewService(nil, store, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.SetPeripheralScheduleHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPut, "/", strings.NewReader(schedule)), "user-1"),
			map[string]string{"id": "dev-1", "name": "ghost"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "peripheral_not_found")
	})

	t.Run("invalid body returns 400", func(t *testing.T) {
		svc := peripheral.NewService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.SetPeripheralScheduleHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPut, "/", strings.NewReader("not json")), "user-1"),
			map[string]string{"id": "dev-1", "name": "light"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})
}

// ── CreatePeripheralHandler ───────────────────────────────────────────────────

func TestCreatePeripheralHandler(t *testing.T) {
	t.Run("invalid body returns 400", func(t *testing.T) {
		svc := peripheral.NewService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.CreatePeripheralHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not json")), "user-1"),
			"id", "dev-1",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		svc := peripheral.NewService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.CreatePeripheralHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"kind":"relay","pin":5}`)), "user-1"),
			"id", "dev-1",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("already exists returns 409", func(t *testing.T) {
		store := &stubPeripheralStore{createErr: peripheral.ErrAlreadyExists}
		svc := newPeripheralService(t, store, &stubPublisher{})
		modelStore := &stubDeviceModelStore{
			model: devicemodel.DeviceModel{ID: "model-1"},
			port:  devicemodel.Port{ID: "port-1", Kind: "relay", Label: "RELAY 1", Pin: 16},
		}
		h := &api.CreatePeripheralHandler{Service: svc, ModelStore: modelStore}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"light","kind":"relay","port_id":"port-1"}`)), "user-1"),
			"id", "dev-1",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusConflict, "peripheral_name_conflict")
	})

	t.Run("device not found returns 404", func(t *testing.T) {
		store := &stubPeripheralStore{}
		svc := newPeripheralService(t, store, &stubPublisher{})
		modelStore := &stubDeviceModelStore{modelErr: devicemodel.ErrNotFound}
		h := &api.CreatePeripheralHandler{Service: svc, ModelStore: modelStore}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"light","kind":"relay","port_id":"port-1"}`)), "user-1"),
			"id", "dev-x",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "device_not_found")
	})

	t.Run("purpose is stored and echoed in response", func(t *testing.T) {
		p := newPeripheral("heater")
		purpose := "heater"
		p.Purpose = &purpose
		store := &stubPeripheralStore{created: p}
		svc := newPeripheralService(t, store, &stubPublisher{})
		modelStore := &stubDeviceModelStore{
			model: devicemodel.DeviceModel{ID: "model-1"},
			port:  devicemodel.Port{ID: "port-1", Kind: "relay", Label: "RELAY 1", Pin: 16},
		}
		h := &api.CreatePeripheralHandler{Service: svc, ModelStore: modelStore}

		body := `{"name":"heater","kind":"relay","port_id":"port-1","purpose":"heater"}`
		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"),
			"id", "dev-1",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["purpose"] != "heater" {
			t.Errorf("expected purpose=heater, got %v", resp["purpose"])
		}
	})
}

// ── PatchPeripheralHandler ────────────────────────────────────────────────────

func TestPatchPeripheralHandler(t *testing.T) {
	t.Run("returns 200 with updated peripheral", func(t *testing.T) {
		p := newPeripheral("renamed")
		store := &stubPeripheralStore{updated: p}
		svc := peripheral.NewService(nil, store, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.PatchPeripheralHandler{Service: svc}

		body := `{"name":"renamed"}`
		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)), "user-1"),
			map[string]string{"id": "dev-1", "peripheralId": "pid-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["name"] != "renamed" {
			t.Errorf("unexpected name: %v", resp["name"])
		}
	})

	t.Run("peripheral not found returns 404", func(t *testing.T) {
		store := &stubPeripheralStore{updateErr: peripheral.ErrNotFound}
		svc := peripheral.NewService(nil, store, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.PatchPeripheralHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"name":"x"}`)), "user-1"),
			map[string]string{"id": "dev-1", "peripheralId": "ghost"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "peripheral_not_found")
	})

	t.Run("name conflict returns 409", func(t *testing.T) {
		store := &stubPeripheralStore{updateErr: peripheral.ErrAlreadyExists}
		svc := peripheral.NewService(nil, store, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.PatchPeripheralHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"name":"taken"}`)), "user-1"),
			map[string]string{"id": "dev-1", "peripheralId": "pid-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusConflict, "peripheral_name_conflict")
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		svc := peripheral.NewService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.PatchPeripheralHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{}`)), "user-1"),
			map[string]string{"id": "dev-1", "peripheralId": "pid-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("invalid body returns 400", func(t *testing.T) {
		svc := peripheral.NewService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.PatchPeripheralHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader("not json")), "user-1"),
			map[string]string{"id": "dev-1", "peripheralId": "pid-1"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})
}

// ── SetControlModeHandler ─────────────────────────────────────────────────────

func TestSetControlModeHandler(t *testing.T) {
	t.Run("returns 200 with updated peripheral", func(t *testing.T) {
		p := newPeripheral("light")
		mode := "manual"
		p.ControlMode = &mode
		store := &stubPeripheralStore{controlModeP: p}
		svc := newPeripheralService(t, store, &stubPublisher{})
		h := &api.SetControlModeHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"mode":"manual"}`)), "user-1"),
			map[string]string{"id": "dev-1", "name": "light"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["control_mode"] != "manual" {
			t.Errorf("expected control_mode=manual, got %v", resp["control_mode"])
		}
	})

	t.Run("invalid mode returns 400", func(t *testing.T) {
		svc := peripheral.NewService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.SetControlModeHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"mode":"unknown"}`)), "user-1"),
			map[string]string{"id": "dev-1", "name": "light"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing mode returns 400", func(t *testing.T) {
		svc := peripheral.NewService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, nil, &stubPublisher{}, discardLogger)
		h := &api.SetControlModeHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{}`)), "user-1"),
			map[string]string{"id": "dev-1", "name": "light"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("peripheral not found returns 404", func(t *testing.T) {
		store := &stubPeripheralStore{controlModeErr: peripheral.ErrNotFound}
		svc := newPeripheralService(t, store, &stubPublisher{})
		h := &api.SetControlModeHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"mode":"manual"}`)), "user-1"),
			map[string]string{"id": "dev-1", "name": "ghost"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "peripheral_not_found")
	})

	t.Run("sensor peripheral returns 422", func(t *testing.T) {
		store := &stubPeripheralStore{controlModeErr: peripheral.ErrNotAnActuator}
		svc := newPeripheralService(t, store, &stubPublisher{})
		h := &api.SetControlModeHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"mode":"manual"}`)), "user-1"),
			map[string]string{"id": "dev-1", "name": "temp"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusUnprocessableEntity, "not_an_actuator")
	})

}

// ── DeletePeripheralHandler ───────────────────────────────────────────────────

func TestDeletePeripheralHandler(t *testing.T) {
	t.Run("peripheral not found returns 404", func(t *testing.T) {
		store := &stubPeripheralStore{deleteErr: peripheral.ErrNotFound}
		svc := newPeripheralService(t, store, &stubPublisher{})
		h := &api.DeletePeripheralHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodDelete, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "name": "ghost"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "peripheral_not_found")
	})
}

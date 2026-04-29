package sensors_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/sensors"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

func newPeripheral(name string) sensors.Peripheral {
	mode := "automatic"
	return sensors.Peripheral{
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

// ── ListPeripheralsHandler ────────────────────────────────────────────────────

func TestListPeripheralsHandler(t *testing.T) {
	t.Run("returns 200 with peripheral list", func(t *testing.T) {
		store := &stubPeripheralStore{listed: []sensors.Peripheral{newPeripheral("light")}}
		svc := sensors.NewPeripheralService(nil, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.ListPeripheralsHandler{Service: svc}

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
		store := &stubPeripheralStore{listed: []sensors.Peripheral{}}
		svc := sensors.NewPeripheralService(nil, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.ListPeripheralsHandler{Service: svc}

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

	t.Run("no auth returns 401", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.ListPeripheralsHandler{Service: svc}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	})
}

// ── SetPeripheralScheduleHandler ─────────────────────────────────────────────

func TestSetPeripheralScheduleHandler(t *testing.T) {
	schedule := `[{"from":"08:00","to":"18:00","value":1.0}]`

	t.Run("returns 200 with updated peripheral", func(t *testing.T) {
		p := newPeripheral("light")
		p.Schedule = []sensors.ScheduleWindow{{From: "08:00", To: "18:00", Value: 1.0}}
		store := &stubPeripheralStore{scheduled: p}
		svc := sensors.NewPeripheralService(nil, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.SetPeripheralScheduleHandler{Service: svc}

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
		store := &stubPeripheralStore{schedErr: sensors.ErrPeripheralNotFound}
		svc := sensors.NewPeripheralService(nil, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.SetPeripheralScheduleHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPut, "/", strings.NewReader(schedule)), "user-1"),
			map[string]string{"id": "dev-1", "name": "ghost"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "peripheral_not_found")
	})

	t.Run("invalid body returns 400", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.SetPeripheralScheduleHandler{Service: svc}

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
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.CreatePeripheralHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not json")), "user-1"),
			"id", "dev-1",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.CreatePeripheralHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"kind":"relay","pin":5}`)), "user-1"),
			"id", "dev-1",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.CreatePeripheralHandler{Service: svc}
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"light","kind":"relay","pin":5}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("already exists returns 409", func(t *testing.T) {
		db := testutil.NewTestDB(t)
		store := &stubPeripheralStore{createErr: sensors.ErrPeripheralAlreadyExists}
		svc := sensors.NewPeripheralService(db, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.CreatePeripheralHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"light","kind":"relay","pin":5}`)), "user-1"),
			"id", "dev-1",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusConflict, "peripheral_name_conflict")
	})

	t.Run("device not found returns 404", func(t *testing.T) {
		db := testutil.NewTestDB(t)
		store := &stubPeripheralStore{createErr: sensors.ErrDeviceNotFound}
		svc := sensors.NewPeripheralService(db, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.CreatePeripheralHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"light","kind":"relay","pin":5}`)), "user-1"),
			"id", "dev-x",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "device_not_found")
	})
}

// ── SetControlModeHandler ─────────────────────────────────────────────────────

func TestSetControlModeHandler(t *testing.T) {
	t.Run("returns 200 with updated peripheral", func(t *testing.T) {
		p := newPeripheral("light")
		mode := "manual"
		p.ControlMode = &mode
		db := testutil.NewTestDB(t)
		store := &stubPeripheralStore{controlModeP: p}
		svc := sensors.NewPeripheralService(db, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.SetControlModeHandler{Service: svc}

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
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.SetControlModeHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"mode":"unknown"}`)), "user-1"),
			map[string]string{"id": "dev-1", "name": "light"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing mode returns 400", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.SetControlModeHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{}`)), "user-1"),
			map[string]string{"id": "dev-1", "name": "light"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("peripheral not found returns 404", func(t *testing.T) {
		db := testutil.NewTestDB(t)
		store := &stubPeripheralStore{controlModeErr: sensors.ErrPeripheralNotFound}
		svc := sensors.NewPeripheralService(db, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.SetControlModeHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"mode":"manual"}`)), "user-1"),
			map[string]string{"id": "dev-1", "name": "ghost"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "peripheral_not_found")
	})

	t.Run("sensor peripheral returns 422", func(t *testing.T) {
		db := testutil.NewTestDB(t)
		store := &stubPeripheralStore{controlModeErr: sensors.ErrNotAnActuator}
		svc := sensors.NewPeripheralService(db, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.SetControlModeHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"mode":"manual"}`)), "user-1"),
			map[string]string{"id": "dev-1", "name": "temp"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusUnprocessableEntity, "not_an_actuator")
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.SetControlModeHandler{Service: svc}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"mode":"manual"}`)))
		assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	})
}

// ── DeletePeripheralHandler ───────────────────────────────────────────────────

func TestDeletePeripheralHandler(t *testing.T) {
	t.Run("no auth returns 401", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.DeletePeripheralHandler{Service: svc}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/", nil))
		assertErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("peripheral not found returns 404", func(t *testing.T) {
		db := testutil.NewTestDB(t)
		store := &stubPeripheralStore{deleteErr: sensors.ErrPeripheralNotFound}
		svc := sensors.NewPeripheralService(db, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.DeletePeripheralHandler{Service: svc}

		req := withChiParams(
			withClaims(httptest.NewRequest(http.MethodDelete, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "name": "ghost"},
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "peripheral_not_found")
	})
}


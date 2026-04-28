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
	return sensors.Peripheral{
		ID:        "pid-1",
		DeviceID:  "dev-1",
		Name:      name,
		Kind:      "relay",
		Pin:       5,
		Schedule:  nil,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
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

	t.Run("device not found returns 404", func(t *testing.T) {
		store := &stubPeripheralStore{listErr: sensors.ErrDeviceNotFound}
		svc := sensors.NewPeripheralService(nil, store, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.ListPeripheralsHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			"id", "dev-1",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.ListPeripheralsHandler{Service: svc}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
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

		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
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

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
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
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
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
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.CreatePeripheralHandler{Service: svc}
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"light","kind":"relay","pin":5}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
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
		if rec.Code != http.StatusConflict {
			t.Errorf("expected 409, got %d", rec.Code)
		}
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
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})
}

// ── DeletePeripheralHandler ───────────────────────────────────────────────────

func TestDeletePeripheralHandler(t *testing.T) {
	t.Run("no auth returns 401", func(t *testing.T) {
		svc := sensors.NewPeripheralService(nil, &stubPeripheralStore{}, &stubOutboxStore{}, &stubPublisher{}, discardLogger)
		h := &sensors.DeletePeripheralHandler{Service: svc}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
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
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rec.Code)
		}
	})
}


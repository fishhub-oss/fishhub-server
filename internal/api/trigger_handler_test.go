package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/api"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
)

func newTrigger() trigger.Trigger {
	actionConfig, _ := json.Marshal(map[string]any{
		"peripheral_id": "peri-1",
		"peripheral":    "relay-14",
		"command":       "set",
		"value":         1.0,
	})
	return trigger.Trigger{
		ID:        "trig-1",
		DeviceID:  "dev-1",
		Name:      "Heater on cold",
		Enabled:   true,
		Condition: json.RawMessage(`{"op":"lt"}`),
		Actions: []trigger.Action{
			{ID: "act-1", Type: "peripheral_action", Config: actionConfig},
		},
		CooldownSeconds: 60,
		CreatedAt:       time.Now(),
	}
}

func newTriggerService(t *testing.T, store *stubTriggerStore) *trigger.Service {
	t.Helper()
	return trigger.NewService(testutil.NewTestDB(t), store, &stubOutboxStore{}, discardLogger)
}

// ── CreateTriggerHandler ──────────────────────────────────────────────────────

func TestCreateTriggerHandler(t *testing.T) {
	// Use valid UUIDs so Postgres doesn't reject the query with a syntax error.
	// The device/peripheral do not exist in the test DB, so the service returns
	// device.ErrNotFound or ErrInvalidPeripheral as appropriate.
	validBody := `{"name":"Heater on cold","condition":{"op":"lt"},"target_peripheral_id":"00000000-0000-0000-0000-000000000001","action":{"action":"set","value":1.0},"cooldown_s":60}`

	t.Run("invalid body returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not json")), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"condition":{"op":"lt"},"target_peripheral_id":"peri-1","action":{"action":"set","value":1.0}}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing condition returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"name":"Heater","target_peripheral_id":"peri-1","action":{"action":"set","value":1.0}}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing target_peripheral_id returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"name":"Heater","condition":{"op":"lt"},"action":{"action":"set","value":1.0}}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing action returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"name":"Heater","condition":{"op":"lt"},"target_peripheral_id":"peri-1"}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("invalid action.action returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"name":"Heater","condition":{"op":"lt"},"target_peripheral_id":"peri-1","action":{"action":"invalid"}}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("negative cooldown_s returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"name":"Heater","condition":{"op":"lt"},"target_peripheral_id":"peri-1","action":{"action":"set","value":1.0},"cooldown_s":-1}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("device not found returns 404", func(t *testing.T) {
		// Both device UUID and user UUID are valid but don't exist in the DB
		// → validatePeripheralAction returns device.ErrNotFound.
		svc := newTriggerService(t, &stubTriggerStore{})
		h := &api.CreateTriggerHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(validBody)), "00000000-0000-0000-0000-000000000099"),
			"id", "00000000-0000-0000-0000-000000000098",
		)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "device_not_found")
	})

	t.Run("invalid peripheral returns 400", func(t *testing.T) {
		db := testutil.NewTestDB(t)
		ctx := context.Background()

		// Insert a real device so the device-exists check passes.
		var deviceID string
		db.QueryRowContext(ctx, `INSERT INTO devices (user_id) VALUES ('00000000-0000-0000-0000-000000000001') RETURNING id`).Scan(&deviceID)

		// peripheral_id in the request refers to a non-existent peripheral → ErrInvalidPeripheral.
		svc := trigger.NewService(db, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"name":"H","condition":{"op":"lt"},"target_peripheral_id":"00000000-0000-0000-0000-000000000099","action":{"action":"set","value":1.0}}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "00000000-0000-0000-0000-000000000001"), "id", deviceID)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})
}

// ── ListTriggersHandler ───────────────────────────────────────────────────────

func TestListTriggersHandler(t *testing.T) {
	t.Run("returns 200 with trigger list", func(t *testing.T) {
		store := &stubTriggerStore{listed: []trigger.Trigger{newTrigger()}}
		svc := trigger.NewService(nil, store, &stubOutboxStore{}, discardLogger)
		h := &api.ListTriggersHandler{Service: svc}

		req := withChiParam(withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp []map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp) != 1 || resp[0]["name"] != "Heater on cold" {
			t.Errorf("unexpected response: %v", resp)
		}
	})

	t.Run("returns empty list when no triggers", func(t *testing.T) {
		store := &stubTriggerStore{listed: []trigger.Trigger{}}
		svc := trigger.NewService(nil, store, &stubOutboxStore{}, discardLogger)
		h := &api.ListTriggersHandler{Service: svc}

		req := withChiParam(withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp []any
		json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
		if len(resp) != 0 {
			t.Errorf("expected empty list, got %v", resp)
		}
	})
}

// ── GetTriggerHandler ─────────────────────────────────────────────────────────

func TestGetTriggerHandler(t *testing.T) {
	t.Run("returns 200 with trigger", func(t *testing.T) {
		store := &stubTriggerStore{got: newTrigger()}
		svc := trigger.NewService(nil, store, &stubOutboxStore{}, discardLogger)
		h := &api.GetTriggerHandler{Service: svc}

		req := withChiParams(withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp["name"] != "Heater on cold" {
			t.Errorf("unexpected name: %v", resp["name"])
		}
	})

	t.Run("not found returns 404", func(t *testing.T) {
		store := &stubTriggerStore{getErr: trigger.ErrNotFound}
		svc := trigger.NewService(nil, store, &stubOutboxStore{}, discardLogger)
		h := &api.GetTriggerHandler{Service: svc}

		req := withChiParams(withClaims(httptest.NewRequest(http.MethodGet, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "ghost"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "trigger_not_found")
	})
}

// ── PatchTriggerHandler ───────────────────────────────────────────────────────

func TestPatchTriggerHandler(t *testing.T) {
	t.Run("invalid body returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.PatchTriggerHandler{Service: svc}

		req := withChiParams(withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader("not json")), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("empty name returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.PatchTriggerHandler{Service: svc}

		req := withChiParams(withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"name":""}`)), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("invalid action.action returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.PatchTriggerHandler{Service: svc}

		body := `{"action":{"action":"invalid"}}`
		req := withChiParams(withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("negative cooldown_s returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.PatchTriggerHandler{Service: svc}

		body := `{"cooldown_s":-5}`
		req := withChiParams(withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("trigger not found returns 404", func(t *testing.T) {
		store := &stubTriggerStore{getErr: trigger.ErrNotFound}
		svc := trigger.NewService(nil, store, &stubOutboxStore{}, discardLogger)
		h := &api.PatchTriggerHandler{Service: svc}

		body := `{"name":"new name"}`
		req := withChiParams(withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)), "user-1"),
			map[string]string{"id": "dev-1", "tid": "ghost"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "trigger_not_found")
	})
}

// ── DeleteTriggerHandler ──────────────────────────────────────────────────────

func TestDeleteTriggerHandler(t *testing.T) {
	t.Run("trigger not found returns 404", func(t *testing.T) {
		store := &stubTriggerStore{deletedErr: trigger.ErrNotFound}
		svc := newTriggerService(t, store)
		h := &api.DeleteTriggerHandler{Service: svc}

		req := withChiParams(withClaims(httptest.NewRequest(http.MethodDelete, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "ghost"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusNotFound, "trigger_not_found")
	})

	t.Run("store error returns 500", func(t *testing.T) {
		store := &stubTriggerStore{deletedErr: errSentinel}
		svc := newTriggerService(t, store)
		h := &api.DeleteTriggerHandler{Service: svc}

		req := withChiParams(withClaims(httptest.NewRequest(http.MethodDelete, "/", nil), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	})
}

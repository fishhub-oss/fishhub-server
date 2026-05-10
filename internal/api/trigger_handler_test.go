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
		"peripheral_id": "00000000-0000-0000-0000-000000000001",
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

// validActionBody is a well-formed actions array for use in request bodies.
const validActionBody = `{"name":"Heater on cold","condition":{"op":"lt"},"actions":[{"type":"peripheral_action","config":{"peripheral_id":"00000000-0000-0000-0000-000000000001","command":"set","value":1.0}}],"cooldown_s":60}`

// ── validateActions ───────────────────────────────────────────────────────────

func TestCreateTriggerHandler_validateActions(t *testing.T) {
	newSvc := func() *trigger.Service {
		return trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
	}
	newH := func() *api.CreateTriggerHandler {
		return &api.CreateTriggerHandler{Service: newSvc()}
	}
	post := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		newH().ServeHTTP(rec, req)
		return rec
	}

	t.Run("zero actions returns 400", func(t *testing.T) {
		body := `{"name":"H","condition":{"op":"lt"},"actions":[]}`
		assertErrorCode(t, post(t, body), http.StatusBadRequest, "invalid_request")
	})

	t.Run("two valid actions passes validation", func(t *testing.T) {
		// Uses a real DB so the service can BeginTx; the peripheral won't exist so it
		// returns 4xx/5xx, but NOT 400 for having two entries.
		svc := newTriggerService(t, &stubTriggerStore{})
		h := &api.CreateTriggerHandler{Service: svc}
		cfg := `{"peripheral_id":"00000000-0000-0000-0000-000000000001","command":"set"}`
		body := `{"name":"H","condition":{"op":"lt"},"actions":[{"type":"peripheral_action","config":` + cfg + `},{"type":"peripheral_action","config":` + cfg + `}]}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusBadRequest {
			t.Errorf("expected non-400 for two valid actions, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown type returns 400", func(t *testing.T) {
		body := `{"name":"H","condition":{"op":"lt"},"actions":[{"type":"unknown","config":{"peripheral_id":"00000000-0000-0000-0000-000000000001","command":"set"}}]}`
		assertErrorCode(t, post(t, body), http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing peripheral_id returns 400", func(t *testing.T) {
		body := `{"name":"H","condition":{"op":"lt"},"actions":[{"type":"peripheral_action","config":{"command":"set"}}]}`
		assertErrorCode(t, post(t, body), http.StatusBadRequest, "invalid_request")
	})

	t.Run("invalid command returns 400", func(t *testing.T) {
		body := `{"name":"H","condition":{"op":"lt"},"actions":[{"type":"peripheral_action","config":{"peripheral_id":"00000000-0000-0000-0000-000000000001","command":"explode"}}]}`
		assertErrorCode(t, post(t, body), http.StatusBadRequest, "invalid_request")
	})
}

// ── CreateTriggerHandler ──────────────────────────────────────────────────────

func TestCreateTriggerHandler(t *testing.T) {
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

		body := `{"condition":{"op":"lt"},"actions":[{"type":"peripheral_action","config":{"peripheral_id":"00000000-0000-0000-0000-000000000001","command":"set"}}]}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing condition returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"name":"H","actions":[{"type":"peripheral_action","config":{"peripheral_id":"00000000-0000-0000-0000-000000000001","command":"set"}}]}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("missing actions returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"name":"H","condition":{"op":"lt"}}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("negative cooldown_s returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		body := `{"name":"H","condition":{"op":"lt"},"actions":[{"type":"peripheral_action","config":{"peripheral_id":"00000000-0000-0000-0000-000000000001","command":"set"}}],"cooldown_s":-1}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "user-1"), "id", "dev-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("device not found returns 404", func(t *testing.T) {
		// Both UUIDs valid but absent from DB → validatePeripheralAction returns device.ErrNotFound.
		svc := newTriggerService(t, &stubTriggerStore{})
		h := &api.CreateTriggerHandler{Service: svc}

		req := withChiParam(
			withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(validActionBody)), "00000000-0000-0000-0000-000000000099"),
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
		db.QueryRowContext(ctx, `INSERT INTO devices (user_id, model_id) VALUES ('00000000-0000-0000-0000-000000000001', (SELECT id FROM device_models WHERE slug = 'fishhub-v1')) RETURNING id`).Scan(&deviceID)

		svc := trigger.NewService(db, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.CreateTriggerHandler{Service: svc}

		// peripheral_id doesn't exist → ErrInvalidPeripheral.
		body := `{"name":"H","condition":{"op":"lt"},"actions":[{"type":"peripheral_action","config":{"peripheral_id":"00000000-0000-0000-0000-000000000099","command":"set"}}]}`
		req := withChiParam(withClaims(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "00000000-0000-0000-0000-000000000001"), "id", deviceID)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})
}

// ── ListTriggersHandler ───────────────────────────────────────────────────────

func TestListTriggersHandler(t *testing.T) {
	t.Run("returns 200 with trigger list including actions", func(t *testing.T) {
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
		actions, ok := resp[0]["actions"].([]any)
		if !ok || len(actions) != 1 {
			t.Errorf("expected actions array with 1 entry, got %v", resp[0]["actions"])
		}
		// Old fields must be absent.
		if _, found := resp[0]["target_peripheral_id"]; found {
			t.Error("target_peripheral_id should not be in response")
		}
		if _, found := resp[0]["action"]; found {
			t.Error("action should not be in response")
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
	t.Run("returns 200 with actions array", func(t *testing.T) {
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
		actions, ok := resp["actions"].([]any)
		if !ok || len(actions) != 1 {
			t.Errorf("expected actions array with 1 entry, got %v", resp["actions"])
		}
		// Old fields must be absent.
		if _, found := resp["target_peripheral_id"]; found {
			t.Error("target_peripheral_id should not be in response")
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

	t.Run("invalid actions in patch returns 400", func(t *testing.T) {
		svc := trigger.NewService(nil, &stubTriggerStore{}, &stubOutboxStore{}, discardLogger)
		h := &api.PatchTriggerHandler{Service: svc}

		body := `{"actions":[{"type":"peripheral_action","config":{"peripheral_id":"00000000-0000-0000-0000-000000000001","command":"explode"}}]}`
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

	t.Run("absent actions field leaves action unchanged", func(t *testing.T) {
		store := &stubTriggerStore{got: newTrigger(), updated: newTrigger()}
		svc := newTriggerService(t, store)
		h := &api.PatchTriggerHandler{Service: svc}

		body := `{"name":"renamed"}`
		req := withChiParams(withClaims(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)), "user-1"),
			map[string]string{"id": "dev-1", "tid": "trig-1"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
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

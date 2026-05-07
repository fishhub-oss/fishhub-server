package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/alerts"
	"github.com/fishhub-oss/fishhub-server/internal/api"
)

// ── stubAlertStore ────────────────────────────────────────────────────────────

type stubAlertStore struct {
	created    alerts.Alert
	createErr  error
	listed     []alerts.Alert
	listErr    error
	cursorRows []alerts.Alert
	cursorErr  error
}

func (s *stubAlertStore) Create(_ context.Context, a alerts.Alert) (alerts.Alert, error) {
	return s.created, s.createErr
}
func (s *stubAlertStore) ListByUser(_ context.Context, _ string, _ int) ([]alerts.Alert, error) {
	return s.listed, s.listErr
}
func (s *stubAlertStore) ListByUserCursor(_ context.Context, _ string, _ alerts.CursorPage) ([]alerts.Alert, error) {
	return s.cursorRows, s.cursorErr
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newAlert(id string, createdAt time.Time) alerts.Alert {
	return alerts.Alert{
		ID:        id,
		UserID:    "user-1",
		DeviceID:  "dev-1",
		TriggerID: "trig-1",
		EventID:   "evt-1",
		Severity:  "warning",
		Message:   "Temperature dropped to 17.2°C",
		Context: map[string]any{
			"peripheral": "ds18b20-4/temperature",
			"value":      17.2,
			"fired_at":   "2025-05-05T14:00:00Z",
		},
		CreatedAt: createdAt,
	}
}

func makeFullAlertPage() []alerts.Alert {
	base := time.Date(2025, 5, 5, 14, 0, 0, 0, time.UTC)
	rows := make([]alerts.Alert, 20)
	for i := range 20 {
		rows[i] = newAlert(fmt.Sprintf("alert-%d", i), base.Add(-time.Duration(i)*time.Minute))
	}
	return rows
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestListAlertsHandler(t *testing.T) {
	createdAt := time.Date(2025, 5, 5, 14, 0, 0, 0, time.UTC)

	t.Run("returns 200 with alerts including resolved unit", func(t *testing.T) {
		store := &stubAlertStore{cursorRows: []alerts.Alert{newAlert("alert-1", createdAt)}}
		h := &api.ListAlertsHandler{Store: store}

		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/alerts", nil), "user-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		rows, ok := resp["alerts"].([]any)
		if !ok || len(rows) != 1 {
			t.Fatalf("expected 1 alert, got %v", resp["alerts"])
		}
		a := rows[0].(map[string]any)
		if a["id"] != "alert-1" {
			t.Errorf("id: got %v", a["id"])
		}
		ctx, ok := a["context"].(map[string]any)
		if !ok {
			t.Fatalf("context not an object: %v", a["context"])
		}
		if ctx["unit"] != "Cel" {
			t.Errorf("unit: got %v, want Cel", ctx["unit"])
		}
		if ctx["peripheral"] != "ds18b20-4/temperature" {
			t.Errorf("peripheral: got %v", ctx["peripheral"])
		}
	})

	t.Run("no next_cursor when fewer than page size rows returned", func(t *testing.T) {
		store := &stubAlertStore{cursorRows: []alerts.Alert{newAlert("a-1", createdAt)}}
		h := &api.ListAlertsHandler{Store: store}

		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/alerts", nil), "user-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
		if _, has := resp["next_cursor"]; has {
			t.Error("expected no next_cursor for partial page")
		}
	})

	t.Run("next_cursor present when exactly page size rows returned", func(t *testing.T) {
		store := &stubAlertStore{cursorRows: makeFullAlertPage()}
		h := &api.ListAlertsHandler{Store: store}

		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/alerts", nil), "user-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
		if _, has := resp["next_cursor"]; !has {
			t.Error("expected next_cursor for full page")
		}
	})

	t.Run("invalid cursor returns 400", func(t *testing.T) {
		store := &stubAlertStore{}
		h := &api.ListAlertsHandler{Store: store}

		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/alerts?cursor=!!invalid!!", nil), "user-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusBadRequest, "invalid_request")
	})

	t.Run("store error returns 500", func(t *testing.T) {
		store := &stubAlertStore{cursorErr: errSentinel}
		h := &api.ListAlertsHandler{Store: store}

		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/alerts", nil), "user-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	})

	t.Run("returns empty alerts array when no rows", func(t *testing.T) {
		store := &stubAlertStore{cursorRows: []alerts.Alert{}}
		h := &api.ListAlertsHandler{Store: store}

		req := withClaims(httptest.NewRequest(http.MethodGet, "/api/alerts", nil), "user-1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
		rows, ok := resp["alerts"].([]any)
		if !ok {
			t.Fatalf("expected alerts array, got %v", resp["alerts"])
		}
		if len(rows) != 0 {
			t.Errorf("expected empty alerts, got %d", len(rows))
		}
	})
}

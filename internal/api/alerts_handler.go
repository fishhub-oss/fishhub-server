package api

import (
	"net/http"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/alerts"
	"github.com/fishhub-oss/fishhub-server/internal/apierr"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/go-chi/render"
)

const defaultAlertPageSize = 20

type AlertContextResponse struct {
	Peripheral string  `json:"peripheral"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit,omitempty"`
	FiredAt    string  `json:"fired_at"`
}

type AlertResponse struct {
	ID        string               `json:"id"`
	DeviceID  string               `json:"device_id"`
	TriggerID string               `json:"trigger_id"`
	EventID   string               `json:"event_id"`
	Severity  string               `json:"severity"`
	Message   string               `json:"message"`
	Context   AlertContextResponse `json:"context"`
	CreatedAt string               `json:"created_at"`
}

type AlertsPageResponse struct {
	Alerts     []AlertResponse `json:"alerts"`
	NextCursor *string         `json:"next_cursor,omitempty"`
}

// ListAlertsHandler handles GET /api/alerts (session auth).
type ListAlertsHandler struct {
	Store alerts.Store
}

func (h *ListAlertsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	page := alerts.CursorPage{PageSize: defaultAlertPageSize}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		ts, id, err := decodeCursor(raw)
		if err != nil {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid cursor")
			return
		}
		page.AfterCreatedAt = &ts
		page.AfterID = &id
	}

	rows, err := h.Store.ListByUserCursor(r.Context(), claims.UserID, page)
	if err != nil {
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := AlertsPageResponse{
		Alerts: make([]AlertResponse, len(rows)),
	}
	for i, a := range rows {
		resp.Alerts[i] = toAlertResponse(a)
	}

	if len(rows) == page.PageSize {
		last := rows[len(rows)-1]
		cursor := encodeCursor(last.CreatedAt, last.ID)
		resp.NextCursor = &cursor
	}

	render.JSON(w, r, resp)
}

func toAlertResponse(a alerts.Alert) AlertResponse {
	ctx := toAlertContextResponse(a.Context)
	return AlertResponse{
		ID:        a.ID,
		DeviceID:  a.DeviceID,
		TriggerID: a.TriggerID,
		EventID:   a.EventID,
		Severity:  a.Severity,
		Message:   a.Message,
		Context:   ctx,
		CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func toAlertContextResponse(ctx map[string]any) AlertContextResponse {
	var peripheral string
	var value float64
	var firedAt string

	if v, ok := ctx["peripheral"].(string); ok {
		peripheral = v
	}
	if v, ok := ctx["value"].(float64); ok {
		value = v
	}
	if v, ok := ctx["fired_at"].(string); ok {
		firedAt = v
	} else if t, ok := ctx["fired_at"].(time.Time); ok {
		firedAt = t.UTC().Format(time.RFC3339)
	}

	unit, _ := alerts.UnitFor(peripheral)

	return AlertContextResponse{
		Peripheral: peripheral,
		Value:      value,
		Unit:       unit,
		FiredAt:    firedAt,
	}
}

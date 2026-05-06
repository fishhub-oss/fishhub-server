package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/apierr"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
	trigger_events "github.com/fishhub-oss/fishhub-server/internal/trigger_events"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

const defaultEventPageSize = 20

type ReadingResponse struct {
	Peripheral string  `json:"peripheral"`
	Value      float64 `json:"value"`
}

type TriggerEventResponse struct {
	ID             string            `json:"id"`
	TriggerEventID string            `json:"trigger_event_id"`
	TriggerID      string            `json:"trigger_id"`
	FiredAt        string            `json:"fired_at"`
	ReceivedAt     string            `json:"received_at"`
	Readings       []ReadingResponse `json:"readings"`
}

type TriggerEventsPageResponse struct {
	Events     []TriggerEventResponse `json:"events"`
	NextCursor *string                `json:"next_cursor,omitempty"`
}

// cursorToken is the JSON payload encoded inside the base64 cursor string.
type cursorToken struct {
	FiredAt time.Time `json:"fired_at"`
	ID      string    `json:"id"`
}

func encodeCursor(firedAt time.Time, id string) string {
	b, _ := json.Marshal(cursorToken{FiredAt: firedAt, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(raw string) (time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", err
	}
	var tok cursorToken
	if err := json.Unmarshal(b, &tok); err != nil {
		return time.Time{}, "", err
	}
	return tok.FiredAt, tok.ID, nil
}

func triggerEventResponse(e trigger_events.TriggerEvent) TriggerEventResponse {
	readings := make([]ReadingResponse, len(e.Readings))
	for i, r := range e.Readings {
		readings[i] = ReadingResponse{Peripheral: r.Peripheral, Value: r.Value}
	}
	return TriggerEventResponse{
		ID:             e.ID,
		TriggerEventID: e.TriggerEventID,
		TriggerID:      e.TriggerID,
		FiredAt:        e.FiredAt.UTC().Format(time.RFC3339),
		ReceivedAt:     e.ReceivedAt.UTC().Format(time.RFC3339),
		Readings:       readings,
	}
}

// ListTriggerEventsHandler handles GET /api/devices/{id}/triggers/{tid}/events (session auth).
type ListTriggerEventsHandler struct {
	TriggerStore trigger.Store
	EventStore   trigger_events.Store
}

func (h *ListTriggerEventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "tid")

	// Parse optional cursor.
	page := trigger_events.CursorPage{PageSize: defaultEventPageSize}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		firedAt, id, err := decodeCursor(raw)
		if err != nil {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid cursor")
			return
		}
		page.AfterFiredAt = &firedAt
		page.AfterID = &id
	}

	// Verify the trigger exists and belongs to this device + user.
	if _, err := h.TriggerStore.Get(r.Context(), deviceID, claims.UserID, triggerID); err != nil {
		if errors.Is(err, trigger.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "trigger_not_found", "trigger not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	events, err := h.EventStore.ListByTriggerCursor(r.Context(), triggerID, page)
	if err != nil {
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := TriggerEventsPageResponse{
		Events: make([]TriggerEventResponse, len(events)),
	}
	for i, e := range events {
		resp.Events[i] = triggerEventResponse(e)
	}

	// Emit next_cursor only when a full page was returned (more may exist).
	if len(events) == page.PageSize {
		last := events[len(events)-1]
		cursor := encodeCursor(last.FiredAt, last.ID)
		resp.NextCursor = &cursor
	}

	render.JSON(w, r, resp)
}

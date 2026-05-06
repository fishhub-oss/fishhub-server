package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/apierr"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
	trigger_events "github.com/fishhub-oss/fishhub-server/internal/trigger_events"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

const (
	defaultEventLimit = 50
	maxEventLimit     = 200
)

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

	limit := defaultEventLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", "limit must be a positive integer")
			return
		}
		if v > maxEventLimit {
			v = maxEventLimit
		}
		limit = v
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

	events, err := h.EventStore.ListByTrigger(r.Context(), triggerID, limit)
	if err != nil {
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := make([]TriggerEventResponse, len(events))
	for i, e := range events {
		resp[i] = triggerEventResponse(e)
	}
	render.JSON(w, r, resp)
}

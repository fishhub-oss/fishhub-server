package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/apierr"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/trigger"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type TriggerResponse struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Enabled            bool            `json:"enabled"`
	Condition          json.RawMessage `json:"condition"`
	TargetPeripheralID string          `json:"target_peripheral_id"`
	Action             json.RawMessage `json:"action"`
	CooldownSeconds    int             `json:"cooldown_s"`
	CreatedAt          string          `json:"created_at"`
}

func triggerResponse(t trigger.Trigger) TriggerResponse {
	return TriggerResponse{
		ID:                 t.ID,
		Name:               t.Name,
		Enabled:            t.Enabled,
		Condition:          t.Condition,
		TargetPeripheralID: t.TargetPeripheralID,
		Action:             t.Action,
		CooldownSeconds:    t.CooldownSeconds,
		CreatedAt:          t.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type createTriggerRequest struct {
	Name               string          `json:"name"`
	Condition          json.RawMessage `json:"condition"`
	TargetPeripheralID string          `json:"target_peripheral_id"`
	Action             json.RawMessage `json:"action"`
	CooldownSeconds    int             `json:"cooldown_s"`
}

// CreateTriggerHandler handles POST /api/devices/{id}/triggers (session auth).
type CreateTriggerHandler struct {
	Service *trigger.Service
}

func (h *CreateTriggerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")

	var req createTriggerRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Name == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	if len(req.Condition) == 0 {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "condition is required")
		return
	}
	if req.TargetPeripheralID == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "target_peripheral_id is required")
		return
	}
	if len(req.Action) == 0 {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "action is required")
		return
	}
	if err := validateTriggerAction(req.Action); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.CooldownSeconds < 0 {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "cooldown_s must be >= 0")
		return
	}

	t, err := h.Service.Create(r.Context(), deviceID, claims.UserID, trigger.TriggerCreate{
		Name:               req.Name,
		Condition:          req.Condition,
		TargetPeripheralID: req.TargetPeripheralID,
		Action:             req.Action,
		CooldownSeconds:    req.CooldownSeconds,
	})
	if err != nil {
		if errors.Is(err, device.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		if errors.Is(err, trigger.ErrInvalidPeripheral) {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", trigger.ErrInvalidPeripheral.Error())
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, triggerResponse(t))
}

// ListTriggersHandler handles GET /api/devices/{id}/triggers (session auth).
type ListTriggersHandler struct {
	Service *trigger.Service
}

func (h *ListTriggersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")

	triggers, err := h.Service.List(r.Context(), deviceID, claims.UserID)
	if err != nil {
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := make([]TriggerResponse, len(triggers))
	for i, t := range triggers {
		resp[i] = triggerResponse(t)
	}
	render.JSON(w, r, resp)
}

// GetTriggerHandler handles GET /api/devices/{id}/triggers/{tid} (session auth).
type GetTriggerHandler struct {
	Service *trigger.Service
}

func (h *GetTriggerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "tid")

	t, err := h.Service.Get(r.Context(), deviceID, claims.UserID, triggerID)
	if err != nil {
		if errors.Is(err, trigger.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "trigger_not_found", "trigger not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, triggerResponse(t))
}

type patchTriggerRequest struct {
	Name            *string         `json:"name"`
	Condition       json.RawMessage `json:"condition"`
	Action          json.RawMessage `json:"action"`
	CooldownSeconds *int            `json:"cooldown_s"`
	Enabled         *bool           `json:"enabled"`
}

// PatchTriggerHandler handles PATCH /api/devices/{id}/triggers/{tid} (session auth).
type PatchTriggerHandler struct {
	Service *trigger.Service
}

func (h *PatchTriggerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "tid")

	var req patchTriggerRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Name != nil && *req.Name == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "name must not be empty")
		return
	}
	if len(req.Action) > 0 {
		if err := validateTriggerAction(req.Action); err != nil {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	if req.CooldownSeconds != nil && *req.CooldownSeconds < 0 {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "cooldown_s must be >= 0")
		return
	}

	// Treat JSON null as absent for JSONB fields.
	condition := req.Condition
	if string(condition) == "null" {
		condition = nil
	}
	action := req.Action
	if string(action) == "null" {
		action = nil
	}

	t, err := h.Service.Update(r.Context(), deviceID, claims.UserID, triggerID, trigger.TriggerPatch{
		Name:            req.Name,
		Condition:       condition,
		Action:          action,
		CooldownSeconds: req.CooldownSeconds,
		Enabled:         req.Enabled,
	})
	if err != nil {
		if errors.Is(err, trigger.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "trigger_not_found", "trigger not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, triggerResponse(t))
}

// DeleteTriggerHandler handles DELETE /api/devices/{id}/triggers/{tid} (session auth).
type DeleteTriggerHandler struct {
	Service *trigger.Service
}

func (h *DeleteTriggerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "tid")

	if err := h.Service.Delete(r.Context(), deviceID, claims.UserID, triggerID); err != nil {
		if errors.Is(err, trigger.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "trigger_not_found", "trigger not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// validateTriggerAction checks that action.action is "set" or "set_mode".
func validateTriggerAction(raw json.RawMessage) error {
	var a struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errors.New("action must be valid JSON")
	}
	if a.Action != "set" && a.Action != "set_mode" {
		return errors.New(`action.action must be "set" or "set_mode"`)
	}
	return nil
}

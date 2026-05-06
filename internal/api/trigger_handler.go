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

type ActionResponse struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

type TriggerResponse struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Enabled         bool             `json:"enabled"`
	Condition       json.RawMessage  `json:"condition"`
	Actions         []ActionResponse `json:"actions"`
	CooldownSeconds int              `json:"cooldown_s"`
	CreatedAt       string           `json:"created_at"`
}

func triggerResponse(t trigger.Trigger) TriggerResponse {
	actions := make([]ActionResponse, len(t.Actions))
	for i, a := range t.Actions {
		actions[i] = ActionResponse{ID: a.ID, Type: a.Type, Config: a.Config}
	}
	return TriggerResponse{
		ID:              t.ID,
		Name:            t.Name,
		Enabled:         t.Enabled,
		Condition:       t.Condition,
		Actions:         actions,
		CooldownSeconds: t.CooldownSeconds,
		CreatedAt:       t.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type actionRequest struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

type createTriggerRequest struct {
	Name            string          `json:"name"`
	Condition       json.RawMessage `json:"condition"`
	Actions         []actionRequest `json:"actions"`
	CooldownSeconds int             `json:"cooldown_s"`
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
	if err := validateActions(req.Actions); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.CooldownSeconds < 0 {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "cooldown_s must be >= 0")
		return
	}

	t, err := h.Service.Create(r.Context(), deviceID, claims.UserID, trigger.TriggerCreate{
		Name:      req.Name,
		Condition: req.Condition,
		Action:    trigger.Action{Type: req.Actions[0].Type, Config: req.Actions[0].Config},
		CooldownSeconds: req.CooldownSeconds,
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
	Actions         []actionRequest `json:"actions"`
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
	if req.CooldownSeconds != nil && *req.CooldownSeconds < 0 {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "cooldown_s must be >= 0")
		return
	}

	// Treat JSON null as absent for JSONB fields.
	condition := req.Condition
	if string(condition) == "null" {
		condition = nil
	}

	patch := trigger.TriggerPatch{
		Name:            req.Name,
		Condition:       condition,
		CooldownSeconds: req.CooldownSeconds,
		Enabled:         req.Enabled,
	}

	if len(req.Actions) > 0 {
		if err := validateActions(req.Actions); err != nil {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		a := trigger.Action{Type: req.Actions[0].Type, Config: req.Actions[0].Config}
		patch.Action = &a
	}

	t, err := h.Service.Update(r.Context(), deviceID, claims.UserID, triggerID, patch)
	if err != nil {
		if errors.Is(err, trigger.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "trigger_not_found", "trigger not found")
			return
		}
		if errors.Is(err, trigger.ErrInvalidPeripheral) {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", trigger.ErrInvalidPeripheral.Error())
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

// validateActions enforces Phase 1 constraints: exactly one peripheral_action with
// a non-empty peripheral_id and a valid command.
func validateActions(actions []actionRequest) error {
	if len(actions) == 0 {
		return errors.New("actions must have exactly one entry")
	}
	if len(actions) > 1 {
		return errors.New("actions must have exactly one entry")
	}
	a := actions[0]
	if a.Type != "peripheral_action" {
		return errors.New(`actions[0].type must be "peripheral_action"`)
	}
	var cfg struct {
		PeripheralID string `json:"peripheral_id"`
		Command      string `json:"command"`
	}
	if err := json.Unmarshal(a.Config, &cfg); err != nil {
		return errors.New("actions[0].config must be valid JSON")
	}
	if cfg.PeripheralID == "" {
		return errors.New("actions[0].config.peripheral_id is required")
	}
	if cfg.Command != "set" && cfg.Command != "set_mode" {
		return errors.New(`actions[0].config.command must be "set" or "set_mode"`)
	}
	return nil
}

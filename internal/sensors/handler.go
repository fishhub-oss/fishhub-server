package sensors

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/apierr"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type DeviceResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// DevicesHandler handles GET /api/devices (session auth).
type DevicesHandler struct {
	Service *DeviceService
}

func (h *DevicesHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	devices, err := h.Service.List(r.Context(), claims.UserID)
	if err != nil {
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := make([]DeviceResponse, len(devices))
	for i, d := range devices {
		resp[i] = DeviceResponse{
			ID:        d.ID,
			Name:      d.Name,
			CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339),
		}
	}
	render.JSON(w, r, resp)
}

// ReadingsHandler handles POST /readings (device JWT auth).
type ReadingsHandler struct {
	Service *ReadingsService
}

func (h *ReadingsHandler) Create(w http.ResponseWriter, r *http.Request) {
	device, ok := DeviceFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "failed to read request body")
		return
	}

	if err := h.Service.Write(r.Context(), device, body); err != nil {
		if errors.Is(err, ErrEmptyPayload) ||
			errors.Is(err, ErrMissingBaseTime) ||
			errors.Is(err, ErrEmptyEntries) {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, ErrInfluxWrite) {
			apierr.Write(w, http.StatusInternalServerError, "internal_error", "failed to persist reading")
			return
		}
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid payload")
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, map[string]string{})
}

// ReadingsQueryHandler handles GET /api/devices/{id}/readings (session auth).
type ReadingsQueryHandler struct {
	Service *ReadingsService
}

type ReadingPointResponse struct {
	Timestamp string         `json:"timestamp"`
	Values    map[string]any `json:"values"`
}

type ReadingsQueryResponse struct {
	DeviceID string                 `json:"device_id"`
	From     string                 `json:"from"`
	To       string                 `json:"to"`
	Readings []ReadingPointResponse `json:"readings"`
}

func (h *ReadingsQueryHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	deviceID := chi.URLParam(r, "id")

	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	to := now
	window := "5m"

	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid 'from' param: must be RFC3339")
			return
		}
		from = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid 'to' param: must be RFC3339")
			return
		}
		to = t
	}
	if v := r.URL.Query().Get("window"); v != "" {
		window = v
	}

	var measurements []string
	if v := r.URL.Query().Get("measurements"); v != "" {
		measurements = strings.Split(v, ",")
	}

	points, err := h.Service.Query(r.Context(), claims.UserID, ReadingQuery{
		DeviceID:     deviceID,
		From:         from,
		To:           to,
		Window:       window,
		Measurements: measurements,
	})
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := ReadingsQueryResponse{
		DeviceID: deviceID,
		From:     from.UTC().Format(time.RFC3339),
		To:       to.UTC().Format(time.RFC3339),
		Readings: make([]ReadingPointResponse, len(points)),
	}
	for i, p := range points {
		resp.Readings[i] = ReadingPointResponse{
			Timestamp: p.Timestamp.UTC().Format(time.RFC3339),
			Values:    p.Values,
		}
	}
	render.JSON(w, r, resp)
}

// DeleteDeviceHandler handles DELETE /api/devices/{id} (session auth).
type DeleteDeviceHandler struct {
	Service *DeviceService
}

func (h *DeleteDeviceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	deviceID := chi.URLParam(r, "id")
	if err := h.Service.Delete(r.Context(), deviceID, claims.UserID); err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PatchDeviceHandler handles PATCH /api/devices/{id} (session auth).
type PatchDeviceHandler struct {
	Service *DeviceService
}

type patchDeviceRequest struct {
	Name string `json:"name"`
}

func (h *PatchDeviceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	var req patchDeviceRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil || req.Name == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}

	deviceID := chi.URLParam(r, "id")
	device, err := h.Service.Patch(r.Context(), deviceID, claims.UserID, req.Name)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, DeviceResponse{
		ID:        device.ID,
		Name:      device.Name,
		CreatedAt: device.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// ProvisionHandler handles POST /api/devices/provision (session auth).
type ProvisionHandler struct {
	Service *ProvisioningService
}

type provisionResponse struct {
	Code string `json:"code"`
}

func (h *ProvisionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	code, err := h.Service.Provision(r.Context(), claims.UserID)
	if err != nil {
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, provisionResponse{Code: code})
}

// ActivateHandler handles POST /devices/activate (no auth — called by the device).
type ActivateHandler struct {
	Service *ActivationService
}

type activateRequest struct {
	Code string `json:"code"`
}

type activateResponse struct {
	Token    string `json:"token"`
	DeviceID string `json:"device_id"`
}

func (h *ActivateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req activateRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil || req.Code == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "code is required")
		return
	}

	result, err := h.Service.Activate(r.Context(), req.Code)
	if err != nil {
		if errors.Is(err, ErrCodeNotFound) {
			apierr.Write(w, http.StatusNotFound, "provisioning_code_not_found", "provisioning code not found")
			return
		}
		if errors.Is(err, ErrCodeAlreadyUsed) {
			apierr.Write(w, http.StatusConflict, "provisioning_code_conflict", "provisioning code already used")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.Status(r, http.StatusAccepted)
	render.JSON(w, r, activateResponse{
		Token:    result.Token,
		DeviceID: result.DeviceID,
	})
}

// ActivationStatusHandler handles GET /devices/{id}/status (device JWT auth).
type ActivationStatusHandler struct {
	Store    DeviceStore
	MQTTHost string
	MQTTPort int
}

type activationStatusResponse struct {
	Status       string `json:"status"`
	MQTTUsername string `json:"mqtt_username,omitempty"`
	MQTTPassword string `json:"mqtt_password,omitempty"`
	MQTTHost     string `json:"mqtt_host,omitempty"`
	MQTTPort     int    `json:"mqtt_port,omitempty"`
}

func (h *ActivationStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	device, ok := DeviceFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	deviceID := chi.URLParam(r, "id")
	if deviceID != device.DeviceID {
		apierr.Write(w, http.StatusForbidden, "forbidden", "access denied")
		return
	}

	status, err := h.Store.GetActivationStatus(r.Context(), deviceID)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	if !status.Ready {
		render.JSON(w, r, activationStatusResponse{Status: "provisioning"})
		return
	}

	render.JSON(w, r, activationStatusResponse{
		Status:       "ready",
		MQTTUsername: status.MQTTUsername,
		MQTTPassword: status.MQTTPassword,
		MQTTHost:     h.MQTTHost,
		MQTTPort:     h.MQTTPort,
	})
}

// CommandPublisher publishes a payload to an MQTT topic.
type CommandPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

type PeripheralResponse struct {
	ID          string           `json:"id"`
	DeviceID    string           `json:"device_id"`
	Name        string           `json:"name"`
	Kind        string           `json:"kind"`
	Pin         int              `json:"pin"`
	Category    string           `json:"category"`
	ControlMode *string          `json:"control_mode"`
	Schedule    []ScheduleWindow `json:"schedule"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`
}

func peripheralResponse(p Peripheral) PeripheralResponse {
	schedule := p.Schedule
	if schedule == nil {
		schedule = []ScheduleWindow{}
	}
	return PeripheralResponse{
		ID:          p.ID,
		DeviceID:    p.DeviceID,
		Name:        p.Name,
		Kind:        p.Kind,
		Pin:         p.Pin,
		Category:    p.Category,
		ControlMode: p.ControlMode,
		Schedule:    schedule,
		CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// CreatePeripheralHandler handles POST /api/devices/{id}/peripherals (session auth).
type CreatePeripheralHandler struct {
	Service *PeripheralService
}

type createPeripheralRequest struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Pin      int    `json:"pin"`
	Category string `json:"category"` // optional; defaults to "sensor"
}

func (h *CreatePeripheralHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	var req createPeripheralRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Name == "" || req.Kind == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "name and kind are required")
		return
	}
	if req.Category == "" {
		req.Category = "sensor"
	}
	if req.Category != "sensor" && req.Category != "actuator" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "category must be 'sensor' or 'actuator'")
		return
	}

	deviceID := chi.URLParam(r, "id")
	p, err := h.Service.Register(r.Context(), deviceID, claims.UserID, req.Name, req.Kind, req.Category, req.Pin)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		if errors.Is(err, ErrPeripheralAlreadyExists) {
			apierr.Write(w, http.StatusConflict, "peripheral_name_conflict", "a peripheral with that name already exists")
			return
		}
		if errors.Is(err, ErrPeripheralPinInUse) {
			apierr.Write(w, http.StatusConflict, "peripheral_pin_conflict", "pin already in use by another peripheral")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, peripheralResponse(p))
}

// ListPeripheralsHandler handles GET /api/devices/{id}/peripherals (session auth).
type ListPeripheralsHandler struct {
	Service *PeripheralService
}

func (h *ListPeripheralsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	deviceID := chi.URLParam(r, "id")
	peripherals, err := h.Service.List(r.Context(), deviceID, claims.UserID)
	if err != nil {
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := make([]PeripheralResponse, len(peripherals))
	for i, p := range peripherals {
		resp[i] = peripheralResponse(p)
	}
	render.JSON(w, r, resp)
}

// SetPeripheralScheduleHandler handles PUT /api/devices/{id}/peripherals/{name}/schedule (session auth).
type SetPeripheralScheduleHandler struct {
	Service *PeripheralService
}

func (h *SetPeripheralScheduleHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	var schedule []ScheduleWindow
	if err := render.DecodeJSON(r.Body, &schedule); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	deviceID := chi.URLParam(r, "id")
	name := chi.URLParam(r, "name")
	p, err := h.Service.SetSchedule(r.Context(), deviceID, claims.UserID, name, schedule)
	if err != nil {
		if errors.Is(err, ErrPeripheralNotFound) {
			apierr.Write(w, http.StatusNotFound, "peripheral_not_found", "peripheral not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, peripheralResponse(p))
}

// DeletePeripheralHandler handles DELETE /api/devices/{id}/peripherals/{name} (session auth).
type DeletePeripheralHandler struct {
	Service *PeripheralService
}

func (h *DeletePeripheralHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	deviceID := chi.URLParam(r, "id")
	name := chi.URLParam(r, "name")
	if err := h.Service.Delete(r.Context(), deviceID, claims.UserID, name); err != nil {
		if errors.Is(err, ErrPeripheralNotFound) {
			apierr.Write(w, http.StatusNotFound, "peripheral_not_found", "peripheral not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SetControlModeHandler handles PATCH /api/devices/{id}/peripherals/{name}/control-mode (session auth).
type SetControlModeHandler struct {
	Service *PeripheralService
}

type setControlModeRequest struct {
	Mode string `json:"mode"`
}

func (h *SetControlModeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	var req setControlModeRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Mode != "automatic" && req.Mode != "manual" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "mode must be 'automatic' or 'manual'")
		return
	}

	deviceID := chi.URLParam(r, "id")
	name := chi.URLParam(r, "name")
	p, err := h.Service.SetControlMode(r.Context(), deviceID, claims.UserID, name, req.Mode)
	if err != nil {
		if errors.Is(err, ErrPeripheralNotFound) {
			apierr.Write(w, http.StatusNotFound, "peripheral_not_found", "peripheral not found")
			return
		}
		if errors.Is(err, ErrNotAnActuator) {
			apierr.Write(w, http.StatusUnprocessableEntity, "not_an_actuator", "control mode is only supported for actuator peripherals")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, peripheralResponse(p))
}

// CommandHandler handles POST /api/devices/{id}/peripherals/{name}/commands (session auth).
type CommandHandler struct {
	Service *DeviceService
}

func (h *CommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	deviceID := chi.URLParam(r, "id")
	peripheralName := chi.URLParam(r, "name")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "failed to read request body")
		return
	}

	if err := h.Service.SendCommand(r.Context(), deviceID, claims.UserID, peripheralName, body); err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		if errors.Is(err, ErrInvalidCommand) {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", ErrInvalidCommand.Error())
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

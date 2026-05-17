package api

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/apierr"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/device"
	"github.com/fishhub-oss/fishhub-server/internal/devicemodel"
	"github.com/fishhub-oss/fishhub-server/internal/measurement"
	"github.com/fishhub-oss/fishhub-server/internal/peripheral"
	"github.com/fishhub-oss/fishhub-server/internal/provisioning"
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
	Service *device.Service
}

func (h *DevicesHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

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

// ReadingsQueryHandler handles GET /api/devices/{id}/readings (session auth).
type ReadingsQueryHandler struct {
	Service *measurement.ReadingsService
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
	claims := auth.MustClaimsFromContext(r.Context())

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

	points, err := h.Service.Query(r.Context(), claims.UserID, measurement.Query{
		DeviceID:     deviceID,
		From:         from,
		To:           to,
		Window:       window,
		Measurements: measurements,
	})
	if err != nil {
		if errors.Is(err, device.ErrNotFound) {
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
	Service *device.Service
}

func (h *DeleteDeviceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	deviceID := chi.URLParam(r, "id")
	if err := h.Service.Delete(r.Context(), deviceID, claims.UserID); err != nil {
		if errors.Is(err, device.ErrNotFound) {
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
	Service *device.Service
}

type patchDeviceRequest struct {
	Name string `json:"name"`
}

func (h *PatchDeviceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	var req patchDeviceRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil || req.Name == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}

	deviceID := chi.URLParam(r, "id")
	d, err := h.Service.Patch(r.Context(), deviceID, claims.UserID, req.Name)
	if err != nil {
		if errors.Is(err, device.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, DeviceResponse{
		ID:        d.ID,
		Name:      d.Name,
		CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// ProvisionHandler handles POST /api/devices/provision (session auth).
type ProvisionHandler struct {
	Service *provisioning.Service
}

type provisionResponse struct {
	Code string `json:"code"`
}

func (h *ProvisionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

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
	Service *provisioning.ActivationService
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
		if errors.Is(err, provisioning.ErrCodeNotFound) {
			apierr.Write(w, http.StatusNotFound, "provisioning_code_not_found", "provisioning code not found")
			return
		}
		if errors.Is(err, provisioning.ErrCodeAlreadyUsed) {
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
	Store    device.Store
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
	deviceInfo, ok := device.FromContext(r.Context())
	if !ok {
		apierr.Write(w, http.StatusUnauthorized, "unauthorized", "missing or invalid credentials")
		return
	}

	deviceID := chi.URLParam(r, "id")
	if deviceID != deviceInfo.DeviceID {
		apierr.Write(w, http.StatusForbidden, "forbidden", "access denied")
		return
	}

	status, err := h.Store.GetActivationStatus(r.Context(), deviceID)
	if err != nil {
		if errors.Is(err, device.ErrNotFound) {
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

type LastReadingResponse struct {
	Timestamp string         `json:"timestamp"`
	Values    map[string]any `json:"values"`
}

type PortResponse struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Pin   int    `json:"pin"`
}

type PeripheralResponse struct {
	ID          string                      `json:"id"`
	DeviceID    string                      `json:"device_id"`
	Name        string                      `json:"name"`
	Kind        string                      `json:"kind"`
	Pin         int                         `json:"pin"`
	Port        *PortResponse               `json:"port"`
	Category    string                      `json:"category"`
	ControlMode *string                     `json:"control_mode"`
	Purpose     *string                     `json:"purpose"`
	Schedule    []peripheral.ScheduleWindow `json:"schedule"`
	LastReading *LastReadingResponse        `json:"last_reading"`
	CreatedAt   string                      `json:"created_at"`
	UpdatedAt   string                      `json:"updated_at"`
}

func peripheralResponse(p peripheral.Peripheral) PeripheralResponse {
	schedule := p.Schedule
	if schedule == nil {
		schedule = []peripheral.ScheduleWindow{}
	}
	var lastReading *LastReadingResponse
	if p.LastReading != nil {
		lastReading = &LastReadingResponse{
			Timestamp: p.LastReading.Timestamp.UTC().Format(time.RFC3339),
			Values:    p.LastReading.Values,
		}
	}
	var port *PortResponse
	if p.Port != nil {
		port = &PortResponse{ID: p.Port.ID, Label: p.Port.Label, Pin: p.Port.Pin}
	}
	return PeripheralResponse{
		ID:          p.ID,
		DeviceID:    p.DeviceID,
		Name:        p.Name,
		Kind:        p.Kind,
		Pin:         p.Pin,
		Port:        port,
		Category:    p.Category,
		ControlMode: p.ControlMode,
		Purpose:     p.Purpose,
		Schedule:    schedule,
		LastReading: lastReading,
		CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// DeviceModelHandler handles GET /api/devices/{id}/model (session auth).
type DeviceModelHandler struct {
	Store devicemodel.Store
}

type portResponse struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Pin   int    `json:"pin"`
}

type deviceModelResponse struct {
	ID    string         `json:"id"`
	Slug  string         `json:"slug"`
	Name  string         `json:"name"`
	Ports []portResponse `json:"ports"`
}

func (h *DeviceModelHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")

	m, err := h.Store.GetByDeviceID(r.Context(), deviceID, claims.UserID)
	if err != nil {
		if errors.Is(err, devicemodel.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	ports := make([]portResponse, len(m.Ports))
	for i, p := range m.Ports {
		ports[i] = portResponse{ID: p.ID, Kind: p.Kind, Label: p.Label, Pin: p.Pin}
	}
	render.JSON(w, r, deviceModelResponse{ID: m.ID, Slug: m.Slug, Name: m.Name, Ports: ports})
}

// CreatePeripheralHandler handles POST /api/devices/{id}/peripherals (session auth).
type CreatePeripheralHandler struct {
	Service    *peripheral.Service
	ModelStore devicemodel.Store
}

type createPeripheralRequest struct {
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	PortID   string  `json:"port_id"`
	Category string  `json:"category"`
	Purpose  *string `json:"purpose"`
}

func (h *CreatePeripheralHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	var req createPeripheralRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Name == "" || req.Kind == "" || req.PortID == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "name, kind, and port_id are required")
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

	// Resolve and validate the port against the device model.
	m, err := h.ModelStore.GetByDeviceID(r.Context(), deviceID, claims.UserID)
	if err != nil {
		if errors.Is(err, devicemodel.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	port, err := h.ModelStore.GetPort(r.Context(), req.PortID, m.ID)
	if err != nil {
		if errors.Is(err, devicemodel.ErrPortNotFound) {
			apierr.Write(w, http.StatusBadRequest, "port_not_found", "port not found or does not belong to this device's model")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	if port.Kind != req.Kind {
		apierr.Write(w, http.StatusBadRequest, "port_kind_mismatch", "port kind does not match the requested peripheral kind")
		return
	}

	p, err := h.Service.Register(r.Context(), deviceID, claims.UserID, req.Name, req.Kind, req.Category, req.Purpose, port)
	if err != nil {
		if errors.Is(err, device.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "device_not_found", "device not found")
			return
		}
		if errors.Is(err, peripheral.ErrAlreadyExists) {
			apierr.Write(w, http.StatusConflict, "peripheral_name_conflict", "a peripheral with that name already exists")
			return
		}
		if errors.Is(err, peripheral.ErrPortInUse) {
			apierr.Write(w, http.StatusConflict, "port_conflict", "port already in use by another peripheral")
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
	Service *peripheral.Service
}

func (h *ListPeripheralsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

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

// SetPeripheralScheduleHandler handles PUT /api/devices/{id}/peripherals/{peripheralId}/schedule (session auth).
type SetPeripheralScheduleHandler struct {
	Service *peripheral.Service
}

func (h *SetPeripheralScheduleHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	var schedule []peripheral.ScheduleWindow
	if err := render.DecodeJSON(r.Body, &schedule); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	deviceID := chi.URLParam(r, "id")
	peripheralID := chi.URLParam(r, "peripheralId")
	p, err := h.Service.SetSchedule(r.Context(), deviceID, claims.UserID, peripheralID, schedule)
	if err != nil {
		if errors.Is(err, peripheral.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "peripheral_not_found", "peripheral not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, peripheralResponse(p))
}

// DeletePeripheralHandler handles DELETE /api/devices/{id}/peripherals/{peripheralId} (session auth).
type DeletePeripheralHandler struct {
	Service *peripheral.Service
}

func (h *DeletePeripheralHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	deviceID := chi.URLParam(r, "id")
	peripheralID := chi.URLParam(r, "peripheralId")
	if err := h.Service.Delete(r.Context(), deviceID, claims.UserID, peripheralID); err != nil {
		if errors.Is(err, peripheral.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "peripheral_not_found", "peripheral not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PatchPeripheralHandler handles PATCH /api/devices/{id}/peripherals/{peripheralId} (session auth).
type PatchPeripheralHandler struct {
	Service *peripheral.Service
}

type patchPeripheralRequest struct {
	Name string `json:"name"`
}

func (h *PatchPeripheralHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	var req patchPeripheralRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Name == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}

	deviceID := chi.URLParam(r, "id")
	peripheralID := chi.URLParam(r, "peripheralId")
	p, err := h.Service.Update(r.Context(), deviceID, claims.UserID, peripheralID, req.Name)
	if err != nil {
		if errors.Is(err, peripheral.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "peripheral_not_found", "peripheral not found")
			return
		}
		if errors.Is(err, peripheral.ErrAlreadyExists) {
			apierr.Write(w, http.StatusConflict, "peripheral_name_conflict", "a peripheral with that name already exists")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, peripheralResponse(p))
}

// SetControlModeHandler handles PATCH /api/devices/{id}/peripherals/{peripheralId}/control-mode (session auth).
type SetControlModeHandler struct {
	Service *peripheral.Service
}

type setControlModeRequest struct {
	Mode string `json:"mode"`
}

func (h *SetControlModeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

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
	peripheralID := chi.URLParam(r, "peripheralId")
	p, err := h.Service.SetControlMode(r.Context(), deviceID, claims.UserID, peripheralID, req.Mode)
	if err != nil {
		if errors.Is(err, peripheral.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "peripheral_not_found", "peripheral not found")
			return
		}
		if errors.Is(err, peripheral.ErrNotAnActuator) {
			apierr.Write(w, http.StatusUnprocessableEntity, "not_an_actuator", "control mode is only supported for actuator peripherals")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, peripheralResponse(p))
}

// CommandHandler handles POST /api/devices/{id}/peripherals/{peripheralId}/commands (session auth).
type CommandHandler struct {
	Service *peripheral.Service
}

func (h *CommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	deviceID := chi.URLParam(r, "id")
	peripheralName := chi.URLParam(r, "peripheralId")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "failed to read request body")
		return
	}

	if err := h.Service.SendCommand(r.Context(), deviceID, claims.UserID, peripheralName, body); err != nil {
		if errors.Is(err, peripheral.ErrNotFound) {
			apierr.Write(w, http.StatusNotFound, "peripheral_not_found", "peripheral not found")
			return
		}
		if errors.Is(err, peripheral.ErrInvalidCommand) {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", peripheral.ErrInvalidCommand.Error())
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

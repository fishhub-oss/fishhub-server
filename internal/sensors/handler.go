package sensors

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

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
	Store DeviceStore
}

func (h *DevicesHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	status := r.URL.Query().Get("status")
	devices, err := h.Store.ListByUserID(r.Context(), claims.UserID, status)
	if err != nil {
		log.Printf("list devices error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	log.Printf("reading: device_id=%s bytes=%d", device.DeviceID, len(body))

	if err := h.Service.Write(r.Context(), device, body); err != nil {
		if errors.Is(err, ErrEmptyPayload) ||
			errors.Is(err, ErrMissingBaseTime) ||
			errors.Is(err, ErrEmptyEntries) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, ErrInfluxWrite) {
			log.Printf("influx write error (device_id=%s): %v", device.DeviceID, err)
			http.Error(w, "failed to persist reading", http.StatusInternalServerError)
			return
		}
		// JSON parse errors and other malformed payload errors.
		http.Error(w, "invalid payload", http.StatusBadRequest)
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
	Timestamp string             `json:"timestamp"`
	Values    map[string]float64 `json:"values"`
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
			http.Error(w, "invalid 'from' param: must be RFC3339", http.StatusBadRequest)
			return
		}
		from = t
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid 'to' param: must be RFC3339", http.StatusBadRequest)
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
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("query readings error (device_id=%s): %v", deviceID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "id")
	if err := h.Service.Delete(r.Context(), deviceID, claims.UserID); err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("delete device error (device_id=%s): %v", deviceID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PatchDeviceHandler handles PATCH /api/devices/{id} (session auth).
type PatchDeviceHandler struct {
	Store DeviceStore
}

type patchDeviceRequest struct {
	Name string `json:"name"`
}

func (h *PatchDeviceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req patchDeviceRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	deviceID := chi.URLParam(r, "id")
	device, err := h.Store.PatchDevice(r.Context(), deviceID, claims.UserID, req.Name)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("patch device error (device_id=%s): %v", deviceID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
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
	Store ProvisioningStore
}

type provisionResponse struct {
	Code     string `json:"code"`
	DeviceID string `json:"device_id"`
}

func (h *ProvisionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	deviceID, code, err := h.Store.GetOrCreatePending(r.Context(), claims.UserID)
	if err != nil {
		log.Printf("get or create pending error (user_id=%s): %v", claims.UserID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, provisionResponse{Code: code, DeviceID: deviceID})
}

// ActivateHandler handles POST /devices/activate (no auth — called by the device).
type ActivateHandler struct {
	Service *ActivationService
}

type activateRequest struct {
	Code string `json:"code"`
}

type activateResponse struct {
	Token        string `json:"token"`
	DeviceID     string `json:"device_id"`
	MQTTUsername string `json:"mqtt_username,omitempty"`
	MQTTPassword string `json:"mqtt_password,omitempty"`
	MQTTHost     string `json:"mqtt_host,omitempty"`
	MQTTPort     int    `json:"mqtt_port,omitempty"`
}

func (h *ActivateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req activateRequest
	if err := render.DecodeJSON(r.Body, &req); err != nil || req.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}

	result, err := h.Service.Activate(r.Context(), req.Code)
	if err != nil {
		if errors.Is(err, ErrCodeNotFound) {
			http.Error(w, "provisioning code not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, ErrCodeAlreadyUsed) {
			http.Error(w, "provisioning code already used", http.StatusConflict)
			return
		}
		log.Printf("activation error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, activateResponse{
		Token:        result.Token,
		DeviceID:     result.DeviceID,
		MQTTUsername: result.MQTTUsername,
		MQTTPassword: result.MQTTPassword,
		MQTTHost:     result.MQTTHost,
		MQTTPort:     result.MQTTPort,
	})
}

// CommandPublisher publishes a payload to an MQTT topic.
type CommandPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// CommandHandler handles POST /api/devices/{id}/peripherals/{name}/commands (session auth).
type CommandHandler struct {
	Service *DeviceService
}

func (h *CommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "id")
	peripheralName := chi.URLParam(r, "name")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if err := h.Service.SendCommand(r.Context(), deviceID, claims.UserID, peripheralName, body); err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, ErrInvalidCommand) {
			http.Error(w, ErrInvalidCommand.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("send command error (device_id=%s): %v", deviceID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

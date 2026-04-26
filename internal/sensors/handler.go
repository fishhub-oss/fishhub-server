package sensors

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type DeviceResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

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


type ReadingsHandler struct {
	Writer ReadingWriter
}

type ReadingsQueryHandler struct {
	Querier ReadingQuerier
	Devices DeviceStore
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

	if _, err := h.Devices.FindByIDAndUserID(r.Context(), deviceID, claims.UserID); err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("find device error (device_id=%s): %v", deviceID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	points, err := h.Querier.QueryReadings(r.Context(), ReadingQuery{
		DeviceID:     deviceID,
		From:         from,
		To:           to,
		Window:       window,
		Measurements: measurements,
	})
	if err != nil {
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

	reading, err := ParseSenML(body)
	if err != nil {
		if errors.Is(err, ErrEmptyPayload) ||
			errors.Is(err, ErrMissingBaseTime) ||
			errors.Is(err, ErrEmptyEntries) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	log.Printf("reading: device_id=%s measurements=%d bt=%d",
		device.DeviceID, len(reading.Measurements), reading.BaseTime)

	if h.Writer != nil {
		fields := make(map[string]any, len(reading.Measurements))
		for _, m := range reading.Measurements {
			fields[m.Name] = m.Value
		}
		if err := h.Writer.WriteReading(r.Context(), Reading{
			DeviceID:     device.DeviceID,
			UserID:       device.UserID,
			Timestamp:    time.Unix(reading.BaseTime, 0).UTC(),
			Measurements: fields,
		}); err != nil {
			log.Printf("influx write error: %v", err)
			http.Error(w, "failed to persist reading", http.StatusInternalServerError)
			return
		}
	} else {
		log.Printf("warning: no InfluxDB writer configured, reading not persisted")
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, map[string]string{})
}

// DeleteDeviceHandler handles DELETE /api/devices/{id} (session auth).
type DeleteDeviceHandler struct {
	Store  DeviceStore
	HiveMQ hivemq.Client
}

func (h *DeleteDeviceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "id")
	mqttUsername, err := h.Store.DeleteDevice(r.Context(), deviceID, claims.UserID)
	if err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("delete device error (device_id=%s): %v", deviceID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if mqttUsername != "" {
		if err := h.HiveMQ.DeleteDevice(r.Context(), mqttUsername); err != nil {
			log.Printf("hivemq delete device error (device_id=%s): %v", deviceID, err)
		}
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

// ProvisionHandler handles POST /devices/provision (session auth).
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
	Store    ProvisioningStore
	Signer   devicejwt.Signer
	HiveMQ   hivemq.Client
	MQTTHost string
	MQTTPort int
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

	deviceID, userID, err := h.Store.ClaimCode(r.Context(), req.Code)
	if err != nil {
		if errors.Is(err, ErrCodeNotFound) {
			http.Error(w, "provisioning code not found", http.StatusNotFound)
			return
		}
		if errors.Is(err, ErrCodeAlreadyUsed) {
			http.Error(w, "provisioning code already used", http.StatusConflict)
			return
		}
		log.Printf("claim code error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	mqttUsername := deviceID
	mqttPasswordBytes := make([]byte, 32)
	if _, err := rand.Read(mqttPasswordBytes); err != nil {
		log.Printf("generate mqtt password error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	mqttPassword := hex.EncodeToString(mqttPasswordBytes)

	if err := h.HiveMQ.ProvisionDevice(r.Context(), mqttUsername, mqttPassword); err != nil {
		log.Printf("hivemq provision error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.Store.Activate(r.Context(), deviceID, mqttUsername, mqttPassword); err != nil {
		log.Printf("activate device error (device_id=%s): %v", deviceID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	jwtToken, err := h.Signer.Sign(deviceID, userID)
	if err != nil {
		log.Printf("devicejwt sign error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, activateResponse{
		Token:        jwtToken,
		DeviceID:     deviceID,
		MQTTUsername: mqttUsername,
		MQTTPassword: mqttPassword,
		MQTTHost:     h.MQTTHost,
		MQTTPort:     h.MQTTPort,
	})
}

// CommandPublisher publishes a payload to an MQTT topic.
type CommandPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// CommandHandler handles POST /api/devices/{id}/peripherals/{name}/commands (session auth).
type CommandHandler struct {
	Store     DeviceStore
	Publisher CommandPublisher
}

type commandRequest struct {
	Action string `json:"action"`
}

func (h *CommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	deviceID := chi.URLParam(r, "id")
	peripheralName := chi.URLParam(r, "name")

	if _, err := h.Store.FindByIDAndUserID(r.Context(), deviceID, claims.UserID); err != nil {
		if errors.Is(err, ErrDeviceNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("find device error (device_id=%s): %v", deviceID, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var req commandRequest
	if err := render.DecodeJSON(io.NopCloser(bytes.NewReader(body)), &req); err != nil || (req.Action != "set" && req.Action != "schedule") {
		http.Error(w, "action must be 'set' or 'schedule'", http.StatusBadRequest)
		return
	}

	topic := fmt.Sprintf("fishhub/%s/commands/%s", deviceID, peripheralName)
	if err := h.Publisher.Publish(r.Context(), topic, body); err != nil {
		log.Printf("mqtt publish error: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

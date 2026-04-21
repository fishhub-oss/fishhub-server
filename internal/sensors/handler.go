package sensors

import (
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
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

	devices, err := h.Store.ListByUserID(r.Context(), claims.UserID)
	if err != nil {
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

type TokenResponse struct {
	Token    string `json:"token"`
	DeviceID string `json:"device_id"`
	UserID   string `json:"user_id"`
}

type TokensHandler struct {
	Store  TokenStore
	UserID string
}

func (h *TokensHandler) Create(w http.ResponseWriter, r *http.Request) {
	result, err := h.Store.CreateToken(r.Context(), h.UserID)
	if err != nil {
		http.Error(w, "failed to create token", http.StatusInternalServerError)
		return
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, TokenResponse{
		Token:    result.Token,
		DeviceID: result.DeviceID,
		UserID:   result.UserID,
	})
}

type ReadingsHandler struct {
	Writer ReadingWriter
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

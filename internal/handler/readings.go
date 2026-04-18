package handler

import (
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/influx"
	"github.com/fishhub-oss/fishhub-server/internal/senml"
	"github.com/go-chi/render"
)

type ReadingsHandler struct {
	Writer influx.ReadingWriter
}

func (h *ReadingsHandler) Create(w http.ResponseWriter, r *http.Request) {
	device, ok := auth.DeviceFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	reading, err := senml.Parse(body)
	if err != nil {
		if errors.Is(err, senml.ErrEmptyPayload) ||
			errors.Is(err, senml.ErrMissingBaseTime) ||
			errors.Is(err, senml.ErrEmptyEntries) {
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
		if err := h.Writer.WriteReading(r.Context(), influx.Reading{
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

package handler

import (
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/senml"
	"github.com/go-chi/render"
)

type ReadingsHandler struct{}

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
			errors.Is(err, senml.ErrMissingTemperature) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	log.Printf("reading: device_id=%s temperature=%.2f bt=%d",
		device.DeviceID, reading.Temperature, reading.BaseTime)

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, map[string]string{})
}

package firmware

import (
	"errors"
	"net/http"

	"github.com/fishhub-oss/fishhub-server/internal/apierr"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

// FirmwareStatusHandler handles GET /api/devices/{id}/firmware.
type FirmwareStatusHandler struct{ Service *Service }

func (h *FirmwareStatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")

	status, err := h.Service.GetStatus(r.Context(), deviceID, claims.UserID)
	if errors.Is(err, ErrDeviceNotFound) {
		apierr.Write(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	if err != nil {
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, status)
}

// FirmwareConfirmHandler handles POST /api/devices/{id}/firmware/confirm.
type FirmwareConfirmHandler struct{ Service *Service }

func (h *FirmwareConfirmHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")

	err := h.Service.ConfirmUpdate(r.Context(), deviceID, claims.UserID)
	switch {
	case errors.Is(err, ErrDeviceNotFound):
		apierr.Write(w, http.StatusNotFound, "not_found", "device not found")
	case errors.Is(err, ErrNoRelease):
		apierr.Write(w, http.StatusServiceUnavailable, "no_release", "no firmware release available yet")
	case errors.Is(err, ErrUpdateAlreadyPending):
		apierr.Write(w, http.StatusConflict, "update_pending", "an update is already pending for this device")
	case err != nil:
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// FirmwareRetryHandler handles POST /api/devices/{id}/firmware/retry.
type FirmwareRetryHandler struct{ Service *Service }

func (h *FirmwareRetryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())
	deviceID := chi.URLParam(r, "id")

	err := h.Service.RetryUpdate(r.Context(), deviceID, claims.UserID)
	switch {
	case errors.Is(err, ErrDeviceNotFound):
		apierr.Write(w, http.StatusNotFound, "not_found", "device not found")
	case errors.Is(err, ErrNoRelease):
		apierr.Write(w, http.StatusServiceUnavailable, "no_release", "no firmware release available yet")
	case errors.Is(err, ErrNoPendingUpdate):
		apierr.Write(w, http.StatusConflict, "no_pending_update", "no failed or timed-out update to retry")
	case err != nil:
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

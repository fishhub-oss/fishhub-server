package account

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fishhub-oss/fishhub-server/internal/apierr"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/go-chi/render"
)

type MeHandler struct {
	Service *AccountService
}

type meResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Timezone  string `json:"timezone"`
	CreatedAt string `json:"created_at"`
}

func (h *MeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	account, err := h.Service.Me(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			apierr.Write(w, http.StatusNotFound, "account_not_found", "account not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, meResponse{
		ID:        account.ID,
		UserID:    account.UserID,
		Email:     account.Email,
		Name:      account.Name,
		Timezone:  account.Timezone,
		CreatedAt: account.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

type PatchMeHandler struct {
	Service *AccountService
}

func (h *PatchMeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims := auth.MustClaimsFromContext(r.Context())

	var body struct {
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Timezone == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "timezone is required")
		return
	}

	account, err := h.Service.UpdateTimezone(r.Context(), claims.UserID, body.Timezone)
	if err != nil {
		if errors.Is(err, ErrInvalidTimezone) {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid timezone")
			return
		}
		if errors.Is(err, ErrAccountNotFound) {
			apierr.Write(w, http.StatusNotFound, "account_not_found", "account not found")
			return
		}
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, meResponse{
		ID:        account.ID,
		UserID:    account.UserID,
		Email:     account.Email,
		Name:      account.Name,
		Timezone:  account.Timezone,
		CreatedAt: account.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

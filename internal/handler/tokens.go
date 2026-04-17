package handler

import (
	"net/http"

	"github.com/fishhub-oss/fishhub-server/internal/store"
	"github.com/go-chi/render"
)

type TokenResponse struct {
	Token    string `json:"token"`
	DeviceID string `json:"device_id"`
	UserID   string `json:"user_id"`
}

type TokensHandler struct {
	Store  store.TokenStore
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

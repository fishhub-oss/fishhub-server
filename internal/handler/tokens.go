package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/fishhub-oss/fishhub-server/internal/store"
)

type TokensHandler struct {
	DB     *sql.DB
	UserID string
}

func (h *TokensHandler) Create(w http.ResponseWriter, r *http.Request) {
	result, err := store.CreateToken(r.Context(), h.DB, h.UserID)
	if err != nil {
		http.Error(w, "failed to create token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"token":     result.Token,
		"device_id": result.DeviceID,
		"user_id":   result.UserID,
	})
}

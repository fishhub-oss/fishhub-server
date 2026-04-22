package account

import (
	"errors"
	"net/http"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/go-chi/render"
)

type MeHandler struct {
	Store AccountStore
}

type meResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func (h *MeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	account, err := h.Store.FindByUserID(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			http.Error(w, "account not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	render.JSON(w, r, meResponse{
		ID:        account.ID,
		UserID:    account.UserID,
		Email:     account.Email,
		Name:      account.Name,
		CreatedAt: account.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

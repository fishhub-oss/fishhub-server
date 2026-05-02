package account

import (
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
		CreatedAt: account.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

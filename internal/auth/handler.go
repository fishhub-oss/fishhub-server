package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/render"
)

type VerifyHandler struct {
	Service AuthService
}

type verifyRequest struct {
	Provider string `json:"provider"`
	IDToken  string `json:"id_token"`
}

func (h *VerifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Provider == "" || req.IDToken == "" {
		http.Error(w, "provider and id_token are required", http.StatusBadRequest)
		return
	}

	user, err := h.Service.VerifyAndUpsert(r.Context(), req.Provider, req.IDToken)
	if err != nil {
		if errors.Is(err, ErrUnsupportedProvider) {
			http.Error(w, "unsupported provider", http.StatusUnprocessableEntity)
			return
		}
		if errors.Is(err, ErrInvalidIDToken) {
			http.Error(w, "invalid id token", http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	sessionToken, err := h.Service.IssueSessionJWT(user.ID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	render.JSON(w, r, map[string]string{"token": sessionToken})
}

func Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		MaxAge:   -1,
		HttpOnly: true,
		Path:     "/",
	})
	render.JSON(w, r, map[string]string{})
}

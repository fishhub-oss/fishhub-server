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

	refreshToken, err := h.Service.IssueRefreshToken(r.Context(), user.ID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	render.JSON(w, r, map[string]string{
		"token":         sessionToken,
		"refresh_token": refreshToken,
	})
}

type RefreshHandler struct {
	Service AuthService
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *RefreshHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.RefreshToken == "" {
		http.Error(w, "refresh_token is required", http.StatusBadRequest)
		return
	}

	newRaw, sessionJWT, err := h.Service.RotateRefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) || errors.Is(err, ErrTokenExpired) || errors.Is(err, ErrTokenRevoked) {
			http.Error(w, "invalid or expired refresh token", http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	render.JSON(w, r, map[string]string{
		"token":         sessionJWT,
		"refresh_token": newRaw,
	})
}

type LogoutHandler struct {
	Service AuthService
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *LogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req logoutRequest
	// best-effort decode — missing body or missing field is fine
	json.NewDecoder(r.Body).Decode(&req) //nolint:errcheck

	if req.RefreshToken != "" {
		// ignore revocation errors; the cookie is cleared regardless
		h.Service.RevokeRefreshToken(r.Context(), req.RefreshToken) //nolint:errcheck
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		MaxAge:   -1,
		HttpOnly: true,
		Path:     "/",
	})
	render.JSON(w, r, map[string]string{})
}

// Logout is kept for backwards compatibility with existing routes that use it as a plain handler func.
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

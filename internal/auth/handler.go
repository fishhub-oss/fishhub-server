package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/fishhub-oss/fishhub-server/internal/apierr"
	"github.com/go-chi/render"
)

type VerifyHandler struct {
	service AuthService
	logger  *slog.Logger
}

func NewVerifyHandler(service AuthService, logger *slog.Logger) *VerifyHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &VerifyHandler{service: service, logger: logger}
}

type verifyRequest struct {
	Provider    string `json:"provider"`
	IDToken     string `json:"id_token"`     // OIDC providers (google)
	AccessToken string `json:"access_token"` // OAuth-only providers (github)
}

func (h *VerifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.Provider == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "provider is required")
		return
	}

	var credential string
	switch req.Provider {
	case "github":
		if req.AccessToken == "" {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", "access_token is required for github provider")
			return
		}
		credential = req.AccessToken
	default:
		if req.IDToken == "" {
			apierr.Write(w, http.StatusBadRequest, "invalid_request", "id_token is required")
			return
		}
		credential = req.IDToken
	}

	user, err := h.service.VerifyAndUpsert(r.Context(), req.Provider, credential)
	if err != nil {
		if errors.Is(err, ErrUnsupportedProvider) {
			apierr.Write(w, http.StatusUnprocessableEntity, "invalid_request", "unsupported provider")
			return
		}
		if errors.Is(err, ErrInvalidIDToken) {
			apierr.Write(w, http.StatusUnauthorized, "unauthorized", "invalid id token")
			return
		}
		if errors.Is(err, ErrProviderConflict) {
			apierr.Write(w, http.StatusConflict, "provider_conflict", "an account with this email already exists under a different sign-in method")
			return
		}
		h.logger.Error("auth verify", "error", err)
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	sessionToken, err := h.service.IssueSessionJWT(user.ID)
	if err != nil {
		h.logger.Error("auth verify: issue session jwt", "error", err)
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	refreshToken, err := h.service.IssueRefreshToken(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("auth verify: issue refresh token", "error", err)
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, map[string]string{
		"token":         sessionToken,
		"refresh_token": refreshToken,
	})
}

type RefreshHandler struct {
	service AuthService
	logger  *slog.Logger
}

func NewRefreshHandler(service AuthService, logger *slog.Logger) *RefreshHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &RefreshHandler{service: service, logger: logger}
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *RefreshHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	if req.RefreshToken == "" {
		apierr.Write(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}

	newRaw, sessionJWT, err := h.service.RotateRefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) || errors.Is(err, ErrTokenExpired) || errors.Is(err, ErrTokenRevoked) {
			apierr.Write(w, http.StatusUnauthorized, "unauthorized", "invalid or expired refresh token")
			return
		}
		h.logger.Error("auth refresh", "error", err)
		apierr.Write(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	render.JSON(w, r, map[string]string{
		"token":         sessionJWT,
		"refresh_token": newRaw,
	})
}

type LogoutHandler struct {
	service AuthService
}

func NewLogoutHandler(service AuthService) *LogoutHandler {
	return &LogoutHandler{service: service}
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
		h.service.RevokeRefreshToken(r.Context(), req.RefreshToken) //nolint:errcheck
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

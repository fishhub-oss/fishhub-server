package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"

	"github.com/fishhub-oss/fishhub-server/internal/jwtutil"
)

var ErrUnsupportedProvider = errors.New("unsupported provider")
var ErrInvalidIDToken      = errors.New("invalid id token")

const refreshTokenTTL = 30 * 24 * time.Hour

type OIDCConfig struct {
	Providers    map[string]string
	Store        UserStore
	RefreshStore RefreshTokenStore
	EventHandler UserEventHandler
	Signer       jwtutil.Signer
	JWTTTL       time.Duration
}

type AuthService interface {
	VerifyAndUpsert(ctx context.Context, provider, idToken string) (User, error)
	IssueSessionJWT(userID string) (string, error)
	ValidateSessionJWT(token string) (string, error)
	IssueRefreshToken(ctx context.Context, userID string) (string, error)
	RotateRefreshToken(ctx context.Context, rawToken string) (newRawToken, sessionJWT string, err error)
	RevokeRefreshToken(ctx context.Context, rawToken string) error
}

type oidcService struct {
	verifiers    map[string]*gooidc.IDTokenVerifier
	store        UserStore
	refreshStore RefreshTokenStore
	eventHandler UserEventHandler
	signer       jwtutil.Signer
	jwtTTL       time.Duration
}

func NewOIDCService(ctx context.Context, cfg OIDCConfig) (AuthService, error) {
	verifiers := make(map[string]*gooidc.IDTokenVerifier, len(cfg.Providers))
	for name, clientID := range cfg.Providers {
		if clientID == "" {
			continue
		}
		issuerURL, err := issuerFor(name)
		if err != nil {
			return nil, err
		}
		provider, err := gooidc.NewProvider(ctx, issuerURL)
		if err != nil {
			return nil, fmt.Errorf("oidc provider %q: %w", name, err)
		}
		verifiers[name] = provider.Verifier(&gooidc.Config{ClientID: clientID})
	}
	return &oidcService{
		verifiers:    verifiers,
		store:        cfg.Store,
		refreshStore: cfg.RefreshStore,
		eventHandler: cfg.EventHandler,
		signer:       cfg.Signer,
		jwtTTL:       cfg.JWTTTL,
	}, nil
}

func issuerFor(provider string) (string, error) {
	switch provider {
	case "google":
		return "https://accounts.google.com", nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

func (s *oidcService) VerifyAndUpsert(ctx context.Context, provider, rawIDToken string) (User, error) {
	verifier, ok := s.verifiers[provider]
	if !ok {
		return User{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return User{}, fmt.Errorf("%w: %v", ErrInvalidIDToken, err)
	}

	var claims struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return User{}, fmt.Errorf("extract claims: %w", err)
	}

	user, err := s.store.Upsert(ctx, claims.Email, provider, claims.Sub)
	if err != nil {
		return User{}, err
	}

	if s.eventHandler != nil {
		name := claims.Name
		if name == "" {
			name = claims.Email
		}
		if err := s.eventHandler.OnUserVerified(ctx, user.ID, user.Email, name); err != nil {
			return User{}, fmt.Errorf("user event handler: %w", err)
		}
	}

	return user, nil
}

func (s *oidcService) IssueSessionJWT(userID string) (string, error) {
	now := time.Now()
	signed, err := s.signer.Sign(map[string]any{
		"sub": userID,
		"iat": now.Unix(),
		"exp": now.Add(s.jwtTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}

func (s *oidcService) ValidateSessionJWT(raw string) (string, error) {
	token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.signer.PublicKey(), nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil || !token.Valid {
		return "", fmt.Errorf("invalid session token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid claims")
	}
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", errors.New("missing sub claim")
	}
	return sub, nil
}

func (s *oidcService) IssueRefreshToken(ctx context.Context, userID string) (string, error) {
	raw, hash, err := generateRefreshToken()
	if err != nil {
		return "", err
	}
	_, err = s.refreshStore.Create(ctx, userID, hash, time.Now().Add(refreshTokenTTL))
	if err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}
	return raw, nil
}

func (s *oidcService) RotateRefreshToken(ctx context.Context, rawToken string) (string, string, error) {
	hash := hashToken(rawToken)
	rt, err := s.refreshStore.FindByHash(ctx, hash)
	if err != nil {
		return "", "", err
	}
	if rt.RevokedAt != nil {
		return "", "", ErrTokenRevoked
	}
	if time.Now().After(rt.ExpiresAt) {
		return "", "", ErrTokenExpired
	}

	if err := s.refreshStore.Revoke(ctx, rt.ID); err != nil {
		return "", "", fmt.Errorf("revoke old refresh token: %w", err)
	}

	newRaw, newHash, err := generateRefreshToken()
	if err != nil {
		return "", "", err
	}
	_, err = s.refreshStore.Create(ctx, rt.UserID, newHash, time.Now().Add(refreshTokenTTL))
	if err != nil {
		return "", "", fmt.Errorf("store new refresh token: %w", err)
	}

	sessionJWT, err := s.IssueSessionJWT(rt.UserID)
	if err != nil {
		return "", "", err
	}

	return newRaw, sessionJWT, nil
}

func (s *oidcService) RevokeRefreshToken(ctx context.Context, rawToken string) error {
	hash := hashToken(rawToken)
	rt, err := s.refreshStore.FindByHash(ctx, hash)
	if err != nil {
		return err
	}
	return s.refreshStore.Revoke(ctx, rt.ID)
}

func generateRefreshToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	raw = hex.EncodeToString(b)
	hash = hashToken(raw)
	return raw, hash, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

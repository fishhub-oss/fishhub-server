package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
)

var ErrUnsupportedProvider = errors.New("unsupported provider")
var ErrInvalidIDToken = errors.New("invalid id token")

type OIDCConfig struct {
	// Providers maps provider name (e.g. "google") to its client ID.
	Providers map[string]string
	Store     UserStore
	JWTSecret string
	JWTTTL    time.Duration
}

type AuthService interface {
	VerifyAndUpsert(ctx context.Context, provider, idToken string) (User, error)
	IssueSessionJWT(userID string) (string, error)
	ValidateSessionJWT(token string) (string, error)
}

type oidcService struct {
	verifiers map[string]*gooidc.IDTokenVerifier
	store     UserStore
	jwtSecret []byte
	jwtTTL    time.Duration
}

// NewOIDCService builds verifiers for each configured provider eagerly.
// Providers whose client ID is empty are silently skipped.
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
		verifiers: verifiers,
		store:     cfg.Store,
		jwtSecret: []byte(cfg.JWTSecret),
		jwtTTL:    cfg.JWTTTL,
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
	}
	if err := idToken.Claims(&claims); err != nil {
		return User{}, fmt.Errorf("extract claims: %w", err)
	}

	return s.store.Upsert(ctx, claims.Email, provider, claims.Sub)
}

func (s *oidcService) IssueSessionJWT(userID string) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"iat": now.Unix(),
		"exp": now.Add(s.jwtTTL).Unix(),
	})
	signed, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}

func (s *oidcService) ValidateSessionJWT(raw string) (string, error) {
	token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
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

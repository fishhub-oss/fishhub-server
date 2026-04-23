package devicejwt

import (
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/fishhub-oss/fishhub-server/internal/jwtutil"
)

// Signer issues signed JWTs for devices. It is a domain wrapper over jwtutil.Signer
// that assembles device-specific claims (sub, user_id, iss, iat).
type Signer interface {
	// Sign returns a signed JWT with sub=deviceID and user_id claim.
	// Returns "" when unconfigured.
	Sign(deviceID, userID string) (string, error)
	// PublicKey returns the RSA public key for JWKS serialisation. Returns nil when unconfigured.
	PublicKey() *rsa.PublicKey
	// KID returns the key ID embedded in the JWT header and JWKS entry.
	KID() string
}

type deviceSigner struct {
	inner  jwtutil.Signer
	issuer string
}

// New wraps a jwtutil.Signer with device-specific claim assembly.
func New(inner jwtutil.Signer, issuer string) Signer {
	return &deviceSigner{inner: inner, issuer: issuer}
}

func (s *deviceSigner) Sign(deviceID, userID string) (string, error) {
	token, err := s.inner.Sign(map[string]any{
		"iss":     s.issuer,
		"sub":     deviceID,
		"user_id": userID,
		"iat":     time.Now().Unix(),
		// No exp — known limitation, see fishhub-oss/fishhub-server#43
	})
	if err != nil {
		return "", fmt.Errorf("devicejwt: %w", err)
	}
	return token, nil
}

func (s *deviceSigner) PublicKey() *rsa.PublicKey { return s.inner.PublicKey() }
func (s *deviceSigner) KID() string               { return s.inner.KID() }

// noopSigner is returned when no private key is configured.
type noopSigner struct{}

func NewNoOp() Signer                                   { return &noopSigner{} }
func (n *noopSigner) Sign(_, _ string) (string, error) { return "", nil }
func (n *noopSigner) PublicKey() *rsa.PublicKey         { return nil }
func (n *noopSigner) KID() string                       { return "" }

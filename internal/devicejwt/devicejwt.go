package devicejwt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Signer issues signed JWTs for devices and exposes the public key for JWKS.
type Signer interface {
	// Sign returns a signed JWT with sub=deviceID. Returns "" when unconfigured.
	Sign(deviceID string) (string, error)
	// PublicKey returns the RSA public key for JWKS serialisation. Returns nil when unconfigured.
	PublicKey() *rsa.PublicKey
	// KID returns the key ID embedded in the JWT header and JWKS entry.
	KID() string
	// Issuer returns the iss claim value.
	Issuer() string
}

type rsaSigner struct {
	privateKey *rsa.PrivateKey
	kid        string
	issuer     string
}

// NewRSASigner parses a PEM-encoded RSA private key and returns a Signer.
func NewRSASigner(pemKey, kid, issuer string) (Signer, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("devicejwt: failed to decode PEM block")
	}

	var privateKey *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("devicejwt: parse PKCS1 key: %w", err)
		}
		privateKey = k
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("devicejwt: parse PKCS8 key: %w", err)
		}
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("devicejwt: PKCS8 key is not RSA")
		}
		privateKey = rsaKey
	default:
		return nil, fmt.Errorf("devicejwt: unsupported PEM block type %q", block.Type)
	}

	return &rsaSigner{privateKey: privateKey, kid: kid, issuer: issuer}, nil
}

func (s *rsaSigner) Sign(deviceID string) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": s.issuer,
		"sub": deviceID,
		"iat": now.Unix(),
		// No exp — known limitation, see fishhub-oss/fishhub-server#43
	})
	token.Header["kid"] = s.kid

	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("devicejwt: sign: %w", err)
	}
	return signed, nil
}

func (s *rsaSigner) PublicKey() *rsa.PublicKey { return &s.privateKey.PublicKey }
func (s *rsaSigner) KID() string               { return s.kid }
func (s *rsaSigner) Issuer() string            { return s.issuer }

// noopSigner is returned when DEVICE_JWT_PRIVATE_KEY is not configured.
type noopSigner struct{}

func NewNoOp() Signer                         { return &noopSigner{} }
func (n *noopSigner) Sign(_ string) (string, error) { return "", nil }
func (n *noopSigner) PublicKey() *rsa.PublicKey      { return nil }
func (n *noopSigner) KID() string                    { return "" }
func (n *noopSigner) Issuer() string                 { return "" }

package jwtutil

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// Signer signs JWTs with an arbitrary claims payload and exposes the public key for JWKS.
type Signer interface {
	// Sign returns a signed RS256 JWT containing the provided claims.
	Sign(claims map[string]any) (string, error)
	// PublicKey returns the RSA public key. Returns nil when unconfigured.
	PublicKey() *rsa.PublicKey
	// KID returns the key ID included in the JWT header and JWKS entry.
	KID() string
}

type rsaSigner struct {
	privateKey *rsa.PrivateKey
	kid        string
}

// NewRSASigner parses a PEM-encoded RSA private key and returns a Signer.
func NewRSASigner(pemKey, kid string) (Signer, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("jwtutil: failed to decode PEM block")
	}

	var privateKey *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("jwtutil: parse PKCS1 key: %w", err)
		}
		privateKey = k
	case "PRIVATE KEY":
		k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("jwtutil: parse PKCS8 key: %w", err)
		}
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("jwtutil: PKCS8 key is not RSA")
		}
		privateKey = rsaKey
	default:
		return nil, fmt.Errorf("jwtutil: unsupported PEM block type %q", block.Type)
	}

	return &rsaSigner{privateKey: privateKey, kid: kid}, nil
}

func (s *rsaSigner) Sign(claims map[string]any) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims(claims))
	token.Header["kid"] = s.kid

	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("jwtutil: sign: %w", err)
	}
	return signed, nil
}

func (s *rsaSigner) PublicKey() *rsa.PublicKey { return &s.privateKey.PublicKey }
func (s *rsaSigner) KID() string               { return s.kid }

// noopSigner is returned when no private key is configured.
type noopSigner struct{}

func NewNoOp() Signer                              { return &noopSigner{} }
func (n *noopSigner) Sign(_ map[string]any) (string, error) { return "", nil }
func (n *noopSigner) PublicKey() *rsa.PublicKey              { return nil }
func (n *noopSigner) KID() string                            { return "" }

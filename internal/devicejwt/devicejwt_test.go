package devicejwt_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
	"github.com/fishhub-oss/fishhub-server/internal/jwtutil"
	"github.com/golang-jwt/jwt/v5"
)

func generateTestKey(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return string(pemBytes), key
}

func TestDeviceSigner_Sign(t *testing.T) {
	pemKey, privateKey := generateTestKey(t)
	inner, err := jwtutil.NewRSASigner(pemKey, "kid-1")
	if err != nil {
		t.Fatalf("NewRSASigner: %v", err)
	}
	signer := devicejwt.New(inner, "https://example.com")

	signed, err := signer.Sign("device-uuid", "user-uuid")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed == "" {
		t.Fatal("expected non-empty token")
	}

	token, err := jwt.Parse(signed, func(t *jwt.Token) (any, error) {
		return &privateKey.PublicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		t.Fatalf("jwt.Parse: %v", err)
	}
	if !token.Valid {
		t.Fatal("expected valid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("expected MapClaims")
	}
	if claims["sub"] != "device-uuid" {
		t.Errorf("expected sub=device-uuid, got %v", claims["sub"])
	}
	if claims["user_id"] != "user-uuid" {
		t.Errorf("expected user_id=user-uuid, got %v", claims["user_id"])
	}
	if claims["iss"] != "https://example.com" {
		t.Errorf("expected iss=https://example.com, got %v", claims["iss"])
	}
	if claims["iat"] == nil {
		t.Error("expected iat claim to be set")
	}
	if claims["exp"] != nil {
		t.Error("expected no exp claim (known limitation, see #43)")
	}
	if token.Header["kid"] != "kid-1" {
		t.Errorf("expected kid=kid-1, got %v", token.Header["kid"])
	}
}

func TestDeviceSigner_PublicKey(t *testing.T) {
	pemKey, privateKey := generateTestKey(t)
	inner, err := jwtutil.NewRSASigner(pemKey, "kid-1")
	if err != nil {
		t.Fatalf("NewRSASigner: %v", err)
	}
	signer := devicejwt.New(inner, "https://example.com")

	pub := signer.PublicKey()
	if pub == nil {
		t.Fatal("expected non-nil public key")
	}
	if pub.N.Cmp(privateKey.PublicKey.N) != 0 {
		t.Error("public key modulus does not match")
	}
}

func TestNoOpSigner(t *testing.T) {
	signer := devicejwt.NewNoOp()

	token, err := signer.Sign("device-uuid", "user-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
	if signer.PublicKey() != nil {
		t.Error("expected nil public key")
	}
}

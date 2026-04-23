package devicejwt_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/fishhub-oss/fishhub-server/internal/devicejwt"
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

func TestNewRSASigner_invalidPEM(t *testing.T) {
	_, err := devicejwt.NewRSASigner("not-a-pem", "kid-1", "https://example.com")
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestRSASigner_Sign(t *testing.T) {
	pemKey, privateKey := generateTestKey(t)
	signer, err := devicejwt.NewRSASigner(pemKey, "kid-1", "https://example.com")
	if err != nil {
		t.Fatalf("NewRSASigner: %v", err)
	}

	signed, err := signer.Sign("device-uuid")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed == "" {
		t.Fatal("expected non-empty token")
	}

	// Parse and verify with the public key
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

func TestRSASigner_PublicKey(t *testing.T) {
	pemKey, privateKey := generateTestKey(t)
	signer, err := devicejwt.NewRSASigner(pemKey, "kid-1", "https://example.com")
	if err != nil {
		t.Fatalf("NewRSASigner: %v", err)
	}

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

	token, err := signer.Sign("device-uuid")
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

// --- JWKSHandler ---

func TestJWKSHandler_withSigner(t *testing.T) {
	pemKey, _ := generateTestKey(t)
	signer, err := devicejwt.NewRSASigner(pemKey, "kid-1", "https://example.com")
	if err != nil {
		t.Fatalf("NewRSASigner: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	rec := httptest.NewRecorder()
	(&devicejwt.JWKSHandler{Signer: signer}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(body.Keys))
	}
	k := body.Keys[0]
	if k.Kty != "RSA" {
		t.Errorf("expected kty=RSA, got %q", k.Kty)
	}
	if k.Kid != "kid-1" {
		t.Errorf("expected kid=kid-1, got %q", k.Kid)
	}
	if k.Use != "sig" {
		t.Errorf("expected use=sig, got %q", k.Use)
	}
	if k.Alg != "RS256" {
		t.Errorf("expected alg=RS256, got %q", k.Alg)
	}
	if k.N == "" || k.E == "" {
		t.Error("expected non-empty n and e")
	}
}

func TestJWKSHandler_noOp(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	rec := httptest.NewRecorder()
	(&devicejwt.JWKSHandler{Signer: devicejwt.NewNoOp()}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"keys":[]`) {
		t.Errorf("expected empty keys array, got %s", rec.Body.String())
	}
}

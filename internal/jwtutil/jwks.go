package jwtutil

import (
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
)

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// JWKSHandler serves GET /.well-known/jwks.json for one or more Signers.
type JWKSHandler struct {
	Signers []Signer
}

func (h *JWKSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	keys := make([]jwk, 0, len(h.Signers))
	for _, s := range h.Signers {
		pub := s.PublicKey()
		if pub == nil {
			continue
		}
		keys = append(keys, jwk{
			Kty: "RSA",
			Kid: s.KID(),
			Use: "sig",
			Alg: "RS256",
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	json.NewEncoder(w).Encode(jwkSet{Keys: keys})
}

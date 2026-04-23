package devicejwt

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

// JWKSHandler serves GET /.well-known/jwks.json.
type JWKSHandler struct {
	Signer Signer
}

func (h *JWKSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pub := h.Signer.PublicKey()
	if pub == nil {
		json.NewEncoder(w).Encode(jwkSet{Keys: []jwk{}})
		return
	}

	set := jwkSet{
		Keys: []jwk{
			{
				Kty: "RSA",
				Kid: h.Signer.KID(),
				Use: "sig",
				Alg: "RS256",
				N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			},
		},
	}
	json.NewEncoder(w).Encode(set)
}

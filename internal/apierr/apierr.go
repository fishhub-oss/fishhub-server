package apierr

import (
	"encoding/json"
	"net/http"
)

type body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Write(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body{Code: code, Message: message}) //nolint:errcheck
}

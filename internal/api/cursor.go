package api

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// cursorToken is the JSON payload encoded inside the opaque base64url cursor string.
type cursorToken struct {
	Timestamp time.Time `json:"ts"`
	ID        string    `json:"id"`
}

func encodeCursor(ts time.Time, id string) string {
	b, _ := json.Marshal(cursorToken{Timestamp: ts, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(raw string) (time.Time, string, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", err
	}
	var tok cursorToken
	if err := json.Unmarshal(b, &tok); err != nil {
		return time.Time{}, "", err
	}
	return tok.Timestamp, tok.ID, nil
}

package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/handler"
	"github.com/fishhub-oss/fishhub-server/internal/store"
)

type stubTokenStore struct {
	result store.TokenResult
	err    error
}

func (s *stubTokenStore) CreateToken(_ context.Context, userID string) (store.TokenResult, error) {
	return s.result, s.err
}

func TestTokensHandler_Create_success(t *testing.T) {
	h := &handler.TokensHandler{
		Store: &stubTokenStore{result: store.TokenResult{
			Token:    "abc123",
			DeviceID: "device-uuid",
			UserID:   "user-uuid",
		}},
		UserID: "user-uuid",
	}

	req := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	w := httptest.NewRecorder()
	h.Create(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["token"] != "abc123" {
		t.Errorf("unexpected token: %s", body["token"])
	}
	if body["device_id"] != "device-uuid" {
		t.Errorf("unexpected device_id: %s", body["device_id"])
	}
	if body["user_id"] != "user-uuid" {
		t.Errorf("unexpected user_id: %s", body["user_id"])
	}
}

func TestTokensHandler_Create_storeError(t *testing.T) {
	h := &handler.TokensHandler{
		Store:  &stubTokenStore{err: errors.New("db down")},
		UserID: "user-uuid",
	}

	req := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Result().StatusCode)
	}
}

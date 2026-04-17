package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/handler"
	"github.com/fishhub-oss/fishhub-server/internal/store"
)

func requestWithDevice(method, target, body string, device store.DeviceInfo) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	ctx := context.WithValue(req.Context(), contextKey("device"), device)
	// inject via the exported helper path so we don't duplicate the key
	_ = ctx
	// use auth package to inject properly via a minimal middleware
	return req
}

func withDevice(r *http.Request, info store.DeviceInfo) *http.Request {
	type stubDS struct{ info store.DeviceInfo }
	// Build context directly using the same key auth uses, via a one-shot middleware
	called := false
	var enriched *http.Request
	mw := auth.Authenticator(&deviceStoreStub{info: info})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enriched = r
		called = true
	})
	r.Header.Set("Authorization", "Bearer anytoken")
	w := httptest.NewRecorder()
	mw(inner).ServeHTTP(w, r)
	if !called {
		panic("middleware did not call next")
	}
	return enriched
}

type deviceStoreStub struct{ info store.DeviceInfo }

func (s *deviceStoreStub) LookupByToken(_ context.Context, _ string) (store.DeviceInfo, error) {
	return s.info, nil
}

type contextKey string

const validSenML = `[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"temperature","u":"Cel","v":23.4}]}]`

func TestReadingsHandler_Create(t *testing.T) {
	h := &handler.ReadingsHandler{}
	device := store.DeviceInfo{DeviceID: "device-uuid", UserID: "user-uuid"}

	t.Run("valid payload returns 201", func(t *testing.T) {
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML)), device)
		w := httptest.NewRecorder()
		h.Create(w, req)
		if w.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d", w.Code)
		}
	})

	t.Run("malformed JSON returns 400", func(t *testing.T) {
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(`not json`)), device)
		w := httptest.NewRecorder()
		h.Create(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing base time returns 400", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","e":[{"n":"temperature","u":"Cel","v":23.4}]}]`
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(body)), device)
		w := httptest.NewRecorder()
		h.Create(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing temperature returns 400", func(t *testing.T) {
		body := `[{"bn":"fishhub/device/","bt":1713000000,"e":[{"n":"humidity","u":"%RH","v":55}]}]`
		req := withDevice(httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(body)), device)
		w := httptest.NewRecorder()
		h.Create(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("no device in context returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/readings", strings.NewReader(validSenML))
		w := httptest.NewRecorder()
		h.Create(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

package hivemq_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/hivemq"
)

func TestProvisionDevice(t *testing.T) {
	ctx := context.Background()

	t.Run("calls credentials then role-attach", func(t *testing.T) {
		var calls []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			w.WriteHeader(http.StatusCreated)
		}))
		defer srv.Close()

		c := hivemq.NewAPIClient(srv.URL, "token", "role-id")
		if err := c.ProvisionDevice(ctx, "dev-1", "pass"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(calls) != 2 {
			t.Fatalf("expected 2 calls, got %d: %v", len(calls), calls)
		}
		if calls[0] != "POST /mqtt/credentials" {
			t.Errorf("expected first call POST /mqtt/credentials, got %s", calls[0])
		}
		if calls[1] != "PUT /user/dev-1/roles/role-id/attach" {
			t.Errorf("expected second call PUT /user/dev-1/roles/role-id/attach, got %s", calls[1])
		}
	})

	t.Run("rolls back credential when role-attach fails", func(t *testing.T) {
		var calls []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			if r.Method == http.MethodPut {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)
		}))
		defer srv.Close()

		c := hivemq.NewAPIClient(srv.URL, "token", "role-id")
		if err := c.ProvisionDevice(ctx, "dev-1", "pass"); err == nil {
			t.Fatal("expected error, got nil")
		}

		// Should have: create credential, fail attach, delete credential (rollback)
		if len(calls) != 3 {
			t.Fatalf("expected 3 calls (create, attach, rollback), got %d: %v", len(calls), calls)
		}
		if calls[2] != "DELETE /mqtt/credentials/dev-1" {
			t.Errorf("expected rollback DELETE /mqtt/credentials/dev-1, got %s", calls[2])
		}
	})
}

func TestDeleteDevice(t *testing.T) {
	ctx := context.Background()

	t.Run("calls detach then delete credential", func(t *testing.T) {
		var calls []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		c := hivemq.NewAPIClient(srv.URL, "token", "role-id")
		if err := c.DeleteDevice(ctx, "dev-1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(calls) != 2 {
			t.Fatalf("expected 2 calls, got %d: %v", len(calls), calls)
		}
		if calls[0] != "PUT /user/dev-1/roles/role-id/detach" {
			t.Errorf("expected first call PUT /user/dev-1/roles/role-id/detach, got %s", calls[0])
		}
		if calls[1] != "DELETE /mqtt/credentials/dev-1" {
			t.Errorf("expected second call DELETE /mqtt/credentials/dev-1, got %s", calls[1])
		}
	})
}

func TestNoOpClient(t *testing.T) {
	ctx := context.Background()
	c := hivemq.NewNoOp()

	if err := c.ProvisionDevice(ctx, "dev-1", "pass"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if err := c.DeleteDevice(ctx, "dev-1"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

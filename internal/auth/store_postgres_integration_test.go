package auth_test

import (
	"context"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

func TestPostgresStore_Integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	store := auth.NewPostgresStore(db)
	ctx := context.Background()

	t.Run("Upsert creates a new user", func(t *testing.T) {
		u, err := store.Upsert(ctx, "alice@example.com", "google", "google-sub-alice")
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if u.ID == "" {
			t.Error("expected non-empty ID")
		}
		if u.Email != "alice@example.com" {
			t.Errorf("email: got %q, want %q", u.Email, "alice@example.com")
		}
		if u.Provider != "google" {
			t.Errorf("provider: got %q, want %q", u.Provider, "google")
		}
		if u.ProviderSub != "google-sub-alice" {
			t.Errorf("provider_sub: got %q, want %q", u.ProviderSub, "google-sub-alice")
		}
	})

	t.Run("Upsert is idempotent", func(t *testing.T) {
		first, err := store.Upsert(ctx, "bob@example.com", "google", "google-sub-bob")
		if err != nil {
			t.Fatalf("first upsert: %v", err)
		}
		second, err := store.Upsert(ctx, "bob@example.com", "google", "google-sub-bob")
		if err != nil {
			t.Fatalf("second upsert: %v", err)
		}
		if first.ID != second.ID {
			t.Errorf("expected same ID on second upsert: got %q and %q", first.ID, second.ID)
		}
	})

	t.Run("Upsert updates email on conflict", func(t *testing.T) {
		_, err := store.Upsert(ctx, "old@example.com", "google", "google-sub-carol")
		if err != nil {
			t.Fatalf("initial upsert: %v", err)
		}
		updated, err := store.Upsert(ctx, "new@example.com", "google", "google-sub-carol")
		if err != nil {
			t.Fatalf("update upsert: %v", err)
		}
		if updated.Email != "new@example.com" {
			t.Errorf("email after update: got %q, want %q", updated.Email, "new@example.com")
		}
	})

	t.Run("FindByID returns the correct user", func(t *testing.T) {
		created, err := store.Upsert(ctx, "dave@example.com", "github", "github-sub-dave")
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		found, err := store.FindByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if found.ID != created.ID {
			t.Errorf("ID mismatch: got %q, want %q", found.ID, created.ID)
		}
		if found.Email != created.Email {
			t.Errorf("email mismatch: got %q, want %q", found.Email, created.Email)
		}
	})

	t.Run("FindByID returns ErrUserNotFound for unknown ID", func(t *testing.T) {
		_, err := store.FindByID(ctx, "00000000-0000-0000-0000-000000000000")
		if err != auth.ErrUserNotFound {
			t.Errorf("expected ErrUserNotFound, got %v", err)
		}
	})
}

package account_test

import (
	"context"
	"testing"

	"github.com/fishhub-oss/fishhub-server/internal/account"
	"github.com/fishhub-oss/fishhub-server/internal/auth"
	"github.com/fishhub-oss/fishhub-server/internal/testutil"
)

func TestPostgresAccountStore_Integration(t *testing.T) {
	db := testutil.NewTestDB(t)
	userStore := auth.NewPostgresStore(db)
	store := account.NewPostgresStore(db)
	ctx := context.Background()

	user, err := userStore.Upsert(ctx, "alice@example.com", "google", "google-sub-alice")
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	t.Run("Upsert creates a new account", func(t *testing.T) {
		a, err := store.Upsert(ctx, user.ID, "alice@example.com", "Alice")
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if a.ID == "" {
			t.Error("expected non-empty ID")
		}
		if a.UserID != user.ID {
			t.Errorf("user_id: got %q, want %q", a.UserID, user.ID)
		}
		if a.Email != "alice@example.com" {
			t.Errorf("email: got %q, want %q", a.Email, "alice@example.com")
		}
		if a.Name != "Alice" {
			t.Errorf("name: got %q, want %q", a.Name, "Alice")
		}
	})

	t.Run("Upsert updates email and name on conflict", func(t *testing.T) {
		_, err := store.Upsert(ctx, user.ID, "old@example.com", "Old Name")
		if err != nil {
			t.Fatalf("initial upsert: %v", err)
		}
		updated, err := store.Upsert(ctx, user.ID, "new@example.com", "New Name")
		if err != nil {
			t.Fatalf("update upsert: %v", err)
		}
		if updated.Email != "new@example.com" {
			t.Errorf("email: got %q, want %q", updated.Email, "new@example.com")
		}
		if updated.Name != "New Name" {
			t.Errorf("name: got %q, want %q", updated.Name, "New Name")
		}
	})

	t.Run("FindByUserID returns the correct account", func(t *testing.T) {
		_, err := store.Upsert(ctx, user.ID, "alice@example.com", "Alice")
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		found, err := store.FindByUserID(ctx, user.ID)
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if found.UserID != user.ID {
			t.Errorf("user_id: got %q, want %q", found.UserID, user.ID)
		}
	})

	t.Run("FindByUserID returns ErrAccountNotFound for unknown user", func(t *testing.T) {
		_, err := store.FindByUserID(ctx, "00000000-0000-0000-0000-000000000000")
		if err != account.ErrAccountNotFound {
			t.Errorf("expected ErrAccountNotFound, got %v", err)
		}
	})
}

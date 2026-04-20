package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type postgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) UserStore {
	return &postgresStore{db: db}
}

func (s *postgresStore) Upsert(ctx context.Context, email, provider, providerSub string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO users (email, provider, provider_sub)
		VALUES ($1, $2, $3)
		ON CONFLICT (provider, provider_sub) DO UPDATE
		    SET email = EXCLUDED.email
		RETURNING id, email, provider, provider_sub, created_at
	`, email, provider, providerSub).Scan(&u.ID, &u.Email, &u.Provider, &u.ProviderSub, &u.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("upsert user: %w", err)
	}
	return u, nil
}

func (s *postgresStore) FindByID(ctx context.Context, id string) (User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, email, provider, provider_sub, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(&u.ID, &u.Email, &u.Provider, &u.ProviderSub, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("find user by id: %w", err)
	}
	return u, nil
}

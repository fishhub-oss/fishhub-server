package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
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

type postgresRefreshTokenStore struct {
	db *sql.DB
}

func NewPostgresRefreshTokenStore(db *sql.DB) RefreshTokenStore {
	return &postgresRefreshTokenStore{db: db}
}

func (s *postgresRefreshTokenStore) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (RefreshToken, error) {
	var rt RefreshToken
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, token_hash, expires_at, revoked_at, created_at
	`, userID, tokenHash, expiresAt).Scan(
		&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.RevokedAt, &rt.CreatedAt,
	)
	if err != nil {
		return RefreshToken{}, fmt.Errorf("create refresh token: %w", err)
	}
	return rt, nil
}

func (s *postgresRefreshTokenStore) FindByHash(ctx context.Context, tokenHash string) (RefreshToken, error) {
	var rt RefreshToken
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM refresh_tokens
		WHERE token_hash = $1
	`, tokenHash).Scan(
		&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.RevokedAt, &rt.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RefreshToken{}, ErrTokenNotFound
		}
		return RefreshToken{}, fmt.Errorf("find refresh token by hash: %w", err)
	}
	return rt, nil
}

func (s *postgresRefreshTokenStore) Revoke(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL
	`, id)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke refresh token rows affected: %w", err)
	}
	if n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

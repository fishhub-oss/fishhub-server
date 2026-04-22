package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type postgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) AccountStore {
	return &postgresStore{db: db}
}

func (s *postgresStore) Upsert(ctx context.Context, userID, email, name string) (Account, error) {
	var a Account
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO accounts (user_id, email, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE
		    SET email = EXCLUDED.email,
		        name  = EXCLUDED.name,
		        updated_at = now()
		RETURNING id, user_id, email, name, created_at, updated_at
	`, userID, email, name).Scan(&a.ID, &a.UserID, &a.Email, &a.Name, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return Account{}, fmt.Errorf("upsert account: %w", err)
	}
	return a, nil
}

func (s *postgresStore) FindByUserID(ctx context.Context, userID string) (Account, error) {
	var a Account
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, email, name, created_at, updated_at
		FROM accounts
		WHERE user_id = $1
	`, userID).Scan(&a.ID, &a.UserID, &a.Email, &a.Name, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, ErrAccountNotFound
		}
		return Account{}, fmt.Errorf("find account by user id: %w", err)
	}
	return a, nil
}

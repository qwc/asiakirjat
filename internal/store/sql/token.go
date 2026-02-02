package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

type TokenStore struct {
	db *sqlx.DB
}

func NewTokenStore(db *sqlx.DB) *TokenStore {
	return &TokenStore{db: db}
}

func (s *TokenStore) Create(ctx context.Context, token *database.APIToken) error {
	query := `INSERT INTO api_tokens (user_id, token_hash, name, scopes, expires_at) VALUES (?, ?, ?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		token.UserID, token.TokenHash, token.Name, token.Scopes, token.ExpiresAt)
	if err != nil {
		return fmt.Errorf("creating token: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	token.ID = id
	return nil
}

func (s *TokenStore) GetByHash(ctx context.Context, hash string) (*database.APIToken, error) {
	var token database.APIToken
	query := `SELECT * FROM api_tokens WHERE token_hash = ?`
	if err := s.db.GetContext(ctx, &token, s.db.Rebind(query), hash); err != nil {
		return nil, fmt.Errorf("getting token by hash: %w", err)
	}
	return &token, nil
}

func (s *TokenStore) ListByUser(ctx context.Context, userID int64) ([]database.APIToken, error) {
	var tokens []database.APIToken
	query := `SELECT * FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`
	if err := s.db.SelectContext(ctx, &tokens, s.db.Rebind(query), userID); err != nil {
		return nil, fmt.Errorf("listing tokens: %w", err)
	}
	return tokens, nil
}

func (s *TokenStore) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM api_tokens WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), id)
	if err != nil {
		return fmt.Errorf("deleting token: %w", err)
	}
	return nil
}

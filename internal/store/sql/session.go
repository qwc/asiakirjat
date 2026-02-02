package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

type SessionStore struct {
	db *sqlx.DB
}

func NewSessionStore(db *sqlx.DB) *SessionStore {
	return &SessionStore{db: db}
}

func (s *SessionStore) Create(ctx context.Context, session *database.Session) error {
	query := `INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		session.ID, session.UserID, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	return nil
}

func (s *SessionStore) GetByID(ctx context.Context, id string) (*database.Session, error) {
	var session database.Session
	query := `SELECT * FROM sessions WHERE id = ?`
	if err := s.db.GetContext(ctx, &session, s.db.Rebind(query), id); err != nil {
		return nil, fmt.Errorf("getting session: %w", err)
	}
	return &session, nil
}

func (s *SessionStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), id)
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	return nil
}

func (s *SessionStore) DeleteExpired(ctx context.Context) error {
	query := `DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP`
	_, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("deleting expired sessions: %w", err)
	}
	return nil
}

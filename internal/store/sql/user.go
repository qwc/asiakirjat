package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

type UserStore struct {
	db *sqlx.DB
}

func NewUserStore(db *sqlx.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) Create(ctx context.Context, user *database.User) error {
	query := `INSERT INTO users (username, email, password, auth_source, role, is_robot) VALUES (?, ?, ?, ?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		user.Username, user.Email, user.Password, user.AuthSource, user.Role, user.IsRobot)
	if err != nil {
		return fmt.Errorf("creating user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	user.ID = id
	return nil
}

func (s *UserStore) GetByID(ctx context.Context, id int64) (*database.User, error) {
	var user database.User
	query := `SELECT * FROM users WHERE id = ?`
	if err := s.db.GetContext(ctx, &user, s.db.Rebind(query), id); err != nil {
		return nil, fmt.Errorf("getting user by id: %w", err)
	}
	return &user, nil
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*database.User, error) {
	var user database.User
	query := `SELECT * FROM users WHERE username = ?`
	if err := s.db.GetContext(ctx, &user, s.db.Rebind(query), username); err != nil {
		return nil, fmt.Errorf("getting user by username: %w", err)
	}
	return &user, nil
}

func (s *UserStore) List(ctx context.Context) ([]database.User, error) {
	var users []database.User
	query := `SELECT * FROM users WHERE is_robot = 0 ORDER BY username`
	if err := s.db.SelectContext(ctx, &users, query); err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	return users, nil
}

func (s *UserStore) ListRobots(ctx context.Context) ([]database.User, error) {
	var users []database.User
	query := `SELECT * FROM users WHERE is_robot = 1 ORDER BY username`
	if err := s.db.SelectContext(ctx, &users, query); err != nil {
		return nil, fmt.Errorf("listing robot users: %w", err)
	}
	return users, nil
}

func (s *UserStore) Update(ctx context.Context, user *database.User) error {
	query := `UPDATE users SET username = ?, email = ?, password = ?, auth_source = ?, role = ?, is_robot = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		user.Username, user.Email, user.Password, user.AuthSource, user.Role, user.IsRobot, user.ID)
	if err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	return nil
}

func (s *UserStore) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM users WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), id)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	return nil
}

func (s *UserStore) Count(ctx context.Context) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM users`
	if err := s.db.GetContext(ctx, &count, query); err != nil {
		return 0, fmt.Errorf("counting users: %w", err)
	}
	return count, nil
}

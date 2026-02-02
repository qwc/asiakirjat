package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

type ProjectAccessStore struct {
	db *sqlx.DB
}

func NewProjectAccessStore(db *sqlx.DB) *ProjectAccessStore {
	return &ProjectAccessStore{db: db}
}

func (s *ProjectAccessStore) Grant(ctx context.Context, access *database.ProjectAccess) error {
	query := `INSERT INTO project_access (project_id, user_id, role) VALUES (?, ?, ?)
		ON CONFLICT(project_id, user_id) DO UPDATE SET role = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		access.ProjectID, access.UserID, access.Role, access.Role)
	if err != nil {
		return fmt.Errorf("granting project access: %w", err)
	}
	return nil
}

func (s *ProjectAccessStore) Revoke(ctx context.Context, projectID, userID int64) error {
	query := `DELETE FROM project_access WHERE project_id = ? AND user_id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), projectID, userID)
	if err != nil {
		return fmt.Errorf("revoking project access: %w", err)
	}
	return nil
}

func (s *ProjectAccessStore) GetAccess(ctx context.Context, projectID, userID int64) (*database.ProjectAccess, error) {
	var access database.ProjectAccess
	query := `SELECT * FROM project_access WHERE project_id = ? AND user_id = ?`
	if err := s.db.GetContext(ctx, &access, s.db.Rebind(query), projectID, userID); err != nil {
		return nil, fmt.Errorf("getting project access: %w", err)
	}
	return &access, nil
}

func (s *ProjectAccessStore) ListByProject(ctx context.Context, projectID int64) ([]database.ProjectAccess, error) {
	var access []database.ProjectAccess
	query := `SELECT * FROM project_access WHERE project_id = ?`
	if err := s.db.SelectContext(ctx, &access, s.db.Rebind(query), projectID); err != nil {
		return nil, fmt.Errorf("listing project access: %w", err)
	}
	return access, nil
}

func (s *ProjectAccessStore) ListByUser(ctx context.Context, userID int64) ([]database.ProjectAccess, error) {
	var access []database.ProjectAccess
	query := `SELECT * FROM project_access WHERE user_id = ?`
	if err := s.db.SelectContext(ctx, &access, s.db.Rebind(query), userID); err != nil {
		return nil, fmt.Errorf("listing user access: %w", err)
	}
	return access, nil
}

func (s *ProjectAccessStore) ListAccessibleProjectIDs(ctx context.Context, userID int64) ([]int64, error) {
	var ids []int64
	query := `SELECT project_id FROM project_access WHERE user_id = ?`
	if err := s.db.SelectContext(ctx, &ids, s.db.Rebind(query), userID); err != nil {
		return nil, fmt.Errorf("listing accessible project ids: %w", err)
	}
	return ids, nil
}

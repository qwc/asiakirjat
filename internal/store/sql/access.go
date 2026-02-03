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
	// Default source to 'manual' if not specified
	source := access.Source
	if source == "" {
		source = "manual"
	}

	var query string
	if s.db.DriverName() == "mysql" {
		query = `INSERT INTO project_access (project_id, user_id, role, source) VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE role = ?`
	} else {
		query = `INSERT INTO project_access (project_id, user_id, role, source) VALUES (?, ?, ?, ?)
			ON CONFLICT(project_id, user_id, source) DO UPDATE SET role = ?`
	}
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		access.ProjectID, access.UserID, access.Role, source, access.Role)
	if err != nil {
		return fmt.Errorf("granting project access: %w", err)
	}
	return nil
}

func (s *ProjectAccessStore) Revoke(ctx context.Context, projectID, userID int64) error {
	// Revoke only manual access by default (backward compatible)
	query := `DELETE FROM project_access WHERE project_id = ? AND user_id = ? AND source = 'manual'`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), projectID, userID)
	if err != nil {
		return fmt.Errorf("revoking project access: %w", err)
	}
	return nil
}

func (s *ProjectAccessStore) RevokeBySource(ctx context.Context, projectID, userID int64, source string) error {
	query := `DELETE FROM project_access WHERE project_id = ? AND user_id = ? AND source = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), projectID, userID, source)
	if err != nil {
		return fmt.Errorf("revoking project access by source: %w", err)
	}
	return nil
}

func (s *ProjectAccessStore) GetAccess(ctx context.Context, projectID, userID int64) (*database.ProjectAccess, error) {
	// Return the highest-role access record
	var access database.ProjectAccess
	query := `SELECT * FROM project_access WHERE project_id = ? AND user_id = ?
		ORDER BY CASE role WHEN 'admin' THEN 1 WHEN 'editor' THEN 2 ELSE 3 END LIMIT 1`
	if err := s.db.GetContext(ctx, &access, s.db.Rebind(query), projectID, userID); err != nil {
		return nil, fmt.Errorf("getting project access: %w", err)
	}
	return &access, nil
}

func (s *ProjectAccessStore) GetAccessBySource(ctx context.Context, projectID, userID int64, source string) (*database.ProjectAccess, error) {
	var access database.ProjectAccess
	query := `SELECT * FROM project_access WHERE project_id = ? AND user_id = ? AND source = ?`
	if err := s.db.GetContext(ctx, &access, s.db.Rebind(query), projectID, userID, source); err != nil {
		return nil, fmt.Errorf("getting project access by source: %w", err)
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

func (s *ProjectAccessStore) ListByUserAndSource(ctx context.Context, userID int64, source string) ([]database.ProjectAccess, error) {
	var access []database.ProjectAccess
	query := `SELECT * FROM project_access WHERE user_id = ? AND source = ?`
	if err := s.db.SelectContext(ctx, &access, s.db.Rebind(query), userID, source); err != nil {
		return nil, fmt.Errorf("listing user access by source: %w", err)
	}
	return access, nil
}

func (s *ProjectAccessStore) ListAccessibleProjectIDs(ctx context.Context, userID int64) ([]int64, error) {
	var ids []int64
	query := `SELECT DISTINCT project_id FROM project_access WHERE user_id = ?`
	if err := s.db.SelectContext(ctx, &ids, s.db.Rebind(query), userID); err != nil {
		return nil, fmt.Errorf("listing accessible project ids: %w", err)
	}
	return ids, nil
}

func (s *ProjectAccessStore) GetEffectiveRole(ctx context.Context, projectID, userID int64) (string, error) {
	var access []database.ProjectAccess
	query := `SELECT * FROM project_access WHERE project_id = ? AND user_id = ?`
	if err := s.db.SelectContext(ctx, &access, s.db.Rebind(query), projectID, userID); err != nil {
		return "", fmt.Errorf("getting effective role: %w", err)
	}

	if len(access) == 0 {
		return "", nil
	}

	// Return highest role from all sources (admin > editor > viewer)
	for _, a := range access {
		if a.Role == "admin" {
			return "admin", nil
		}
	}
	for _, a := range access {
		if a.Role == "editor" {
			return "editor", nil
		}
	}
	return "viewer", nil
}

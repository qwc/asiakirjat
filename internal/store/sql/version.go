package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

type VersionStore struct {
	db *sqlx.DB
}

func NewVersionStore(db *sqlx.DB) *VersionStore {
	return &VersionStore{db: db}
}

func (s *VersionStore) Create(ctx context.Context, version *database.Version) error {
	query := `INSERT INTO versions (project_id, tag, storage_path, content_type, uploaded_by) VALUES (?, ?, ?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		version.ProjectID, version.Tag, version.StoragePath, version.ContentType, version.UploadedBy)
	if err != nil {
		return fmt.Errorf("creating version: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	version.ID = id
	return nil
}

func (s *VersionStore) GetByProjectAndTag(ctx context.Context, projectID int64, tag string) (*database.Version, error) {
	var version database.Version
	query := `SELECT * FROM versions WHERE project_id = ? AND tag = ?`
	if err := s.db.GetContext(ctx, &version, s.db.Rebind(query), projectID, tag); err != nil {
		return nil, fmt.Errorf("getting version: %w", err)
	}
	return &version, nil
}

func (s *VersionStore) ListByProject(ctx context.Context, projectID int64) ([]database.Version, error) {
	var versions []database.Version
	query := `SELECT * FROM versions WHERE project_id = ? ORDER BY created_at DESC`
	if err := s.db.SelectContext(ctx, &versions, s.db.Rebind(query), projectID); err != nil {
		return nil, fmt.Errorf("listing versions: %w", err)
	}
	return versions, nil
}

func (s *VersionStore) Update(ctx context.Context, version *database.Version) error {
	query := `UPDATE versions SET storage_path = ?, content_type = ?, uploaded_by = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), version.StoragePath, version.ContentType, version.UploadedBy, version.ID)
	if err != nil {
		return fmt.Errorf("updating version: %w", err)
	}
	return nil
}

func (s *VersionStore) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM versions WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), id)
	if err != nil {
		return fmt.Errorf("deleting version: %w", err)
	}
	return nil
}

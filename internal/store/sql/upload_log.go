package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

type UploadLogStore struct {
	db *sqlx.DB
}

func NewUploadLogStore(db *sqlx.DB) *UploadLogStore {
	return &UploadLogStore{db: db}
}

func (s *UploadLogStore) Create(ctx context.Context, log *database.UploadLog) error {
	query := `INSERT INTO upload_logs (project_id, version_tag, content_type, uploaded_by, is_reupload, filename) VALUES (?, ?, ?, ?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		log.ProjectID, log.VersionTag, log.ContentType, log.UploadedBy, log.IsReupload, log.Filename)
	if err != nil {
		return fmt.Errorf("creating upload log: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	log.ID = id
	return nil
}

func (s *UploadLogStore) ListByProject(ctx context.Context, projectID int64) ([]database.UploadLog, error) {
	var logs []database.UploadLog
	query := `SELECT * FROM upload_logs WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT 50`
	if err := s.db.SelectContext(ctx, &logs, s.db.Rebind(query), projectID); err != nil {
		return nil, fmt.Errorf("listing upload logs: %w", err)
	}
	return logs, nil
}

package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

type ProjectStore struct {
	db *sqlx.DB
}

func NewProjectStore(db *sqlx.DB) *ProjectStore {
	return &ProjectStore{db: db}
}

func (s *ProjectStore) Create(ctx context.Context, project *database.Project) error {
	query := `INSERT INTO projects (slug, name, description, visibility, retention_days) VALUES (?, ?, ?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		project.Slug, project.Name, project.Description, project.Visibility, project.RetentionDays)
	if err != nil {
		return fmt.Errorf("creating project: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	project.ID = id
	return nil
}

func (s *ProjectStore) GetBySlug(ctx context.Context, slug string) (*database.Project, error) {
	var project database.Project
	query := `SELECT id, slug, name, description, visibility, retention_days, created_at, updated_at FROM projects WHERE slug = ?`
	if err := s.db.GetContext(ctx, &project, s.db.Rebind(query), slug); err != nil {
		return nil, fmt.Errorf("getting project by slug: %w", err)
	}
	return &project, nil
}

func (s *ProjectStore) GetByID(ctx context.Context, id int64) (*database.Project, error) {
	var project database.Project
	query := `SELECT id, slug, name, description, visibility, retention_days, created_at, updated_at FROM projects WHERE id = ?`
	if err := s.db.GetContext(ctx, &project, s.db.Rebind(query), id); err != nil {
		return nil, fmt.Errorf("getting project by id: %w", err)
	}
	return &project, nil
}

func (s *ProjectStore) List(ctx context.Context) ([]database.Project, error) {
	var projects []database.Project
	query := `SELECT id, slug, name, description, visibility, retention_days, created_at, updated_at FROM projects ORDER BY name`
	if err := s.db.SelectContext(ctx, &projects, query); err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	return projects, nil
}

func (s *ProjectStore) ListByVisibility(ctx context.Context, visibility string) ([]database.Project, error) {
	var projects []database.Project
	query := `SELECT id, slug, name, description, visibility, retention_days, created_at, updated_at FROM projects WHERE visibility = ? ORDER BY name`
	if err := s.db.SelectContext(ctx, &projects, s.db.Rebind(query), visibility); err != nil {
		return nil, fmt.Errorf("listing projects by visibility: %w", err)
	}
	return projects, nil
}

func (s *ProjectStore) Search(ctx context.Context, q string) ([]database.Project, error) {
	var projects []database.Project
	query := `SELECT id, slug, name, description, visibility, retention_days, created_at, updated_at FROM projects WHERE name LIKE ? OR slug LIKE ? OR description LIKE ? ORDER BY name`
	pattern := "%" + q + "%"
	if err := s.db.SelectContext(ctx, &projects, s.db.Rebind(query), pattern, pattern, pattern); err != nil {
		return nil, fmt.Errorf("searching projects: %w", err)
	}
	return projects, nil
}

func (s *ProjectStore) Update(ctx context.Context, project *database.Project) error {
	query := `UPDATE projects SET slug = ?, name = ?, description = ?, visibility = ?, retention_days = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		project.Slug, project.Name, project.Description, project.Visibility, project.RetentionDays, project.ID)
	if err != nil {
		return fmt.Errorf("updating project: %w", err)
	}
	return nil
}

func (s *ProjectStore) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM projects WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), id)
	if err != nil {
		return fmt.Errorf("deleting project: %w", err)
	}
	return nil
}

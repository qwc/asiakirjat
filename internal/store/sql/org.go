package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

// OrgStore manages organizations, the container above projects (issue #150).
// Orgs never appear in URLs, so an org slug cannot collide with a project's.
type OrgStore struct {
	db *sqlx.DB
}

func NewOrgStore(db *sqlx.DB) *OrgStore {
	return &OrgStore{db: db}
}

func (s *OrgStore) List(ctx context.Context) ([]database.Org, error) {
	var orgs []database.Org
	query := `SELECT * FROM orgs ORDER BY name`
	if err := s.db.SelectContext(ctx, &orgs, query); err != nil {
		return nil, fmt.Errorf("listing orgs: %w", err)
	}
	return orgs, nil
}

func (s *OrgStore) GetByID(ctx context.Context, id int64) (*database.Org, error) {
	var org database.Org
	query := `SELECT * FROM orgs WHERE id = ?`
	if err := s.db.GetContext(ctx, &org, s.db.Rebind(query), id); err != nil {
		return nil, fmt.Errorf("getting org: %w", err)
	}
	return &org, nil
}

func (s *OrgStore) GetBySlug(ctx context.Context, slug string) (*database.Org, error) {
	var org database.Org
	query := `SELECT * FROM orgs WHERE slug = ?`
	if err := s.db.GetContext(ctx, &org, s.db.Rebind(query), slug); err != nil {
		return nil, fmt.Errorf("getting org by slug: %w", err)
	}
	return &org, nil
}

func (s *OrgStore) Create(ctx context.Context, org *database.Org) error {
	query := `INSERT INTO orgs (slug, name, description) VALUES (?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query), org.Slug, org.Name, org.Description)
	if err != nil {
		return fmt.Errorf("creating org: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	org.ID = id
	return nil
}

func (s *OrgStore) Update(ctx context.Context, org *database.Org) error {
	query := `UPDATE orgs SET slug = ?, name = ?, description = ? WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), org.Slug, org.Name, org.Description, org.ID); err != nil {
		return fmt.Errorf("updating org: %w", err)
	}
	return nil
}

// Delete removes an org. It refuses while the org still holds projects: those
// projects would be left pointing at nothing, which serving cannot express.
// Move or delete them first.
func (s *OrgStore) Delete(ctx context.Context, id int64) error {
	count, err := s.CountProjects(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("org still holds %d project(s)", count)
	}
	query := `DELETE FROM orgs WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), id); err != nil {
		return fmt.Errorf("deleting org: %w", err)
	}
	return nil
}

// CountProjects reports how many projects belong to an org, which is what
// makes Delete's refusal explainable in the UI.
func (s *OrgStore) CountProjects(ctx context.Context, id int64) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM projects WHERE org_id = ?`
	if err := s.db.GetContext(ctx, &count, s.db.Rebind(query), id); err != nil {
		return 0, fmt.Errorf("counting projects in org: %w", err)
	}
	return count, nil
}

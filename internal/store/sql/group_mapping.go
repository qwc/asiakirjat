package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

type AuthGroupMappingStore struct {
	db *sqlx.DB
}

func NewAuthGroupMappingStore(db *sqlx.DB) *AuthGroupMappingStore {
	return &AuthGroupMappingStore{db: db}
}

func (s *AuthGroupMappingStore) List(ctx context.Context) ([]database.AuthGroupMapping, error) {
	var mappings []database.AuthGroupMapping
	query := `SELECT * FROM auth_group_mappings ORDER BY auth_source, group_identifier`
	if err := s.db.SelectContext(ctx, &mappings, query); err != nil {
		return nil, fmt.Errorf("listing auth group mappings: %w", err)
	}
	return mappings, nil
}

func (s *AuthGroupMappingStore) ListBySource(ctx context.Context, source string) ([]database.AuthGroupMapping, error) {
	var mappings []database.AuthGroupMapping
	query := `SELECT * FROM auth_group_mappings WHERE auth_source = ? ORDER BY group_identifier`
	if err := s.db.SelectContext(ctx, &mappings, s.db.Rebind(query), source); err != nil {
		return nil, fmt.Errorf("listing auth group mappings by source: %w", err)
	}
	return mappings, nil
}

func (s *AuthGroupMappingStore) GetByID(ctx context.Context, id int64) (*database.AuthGroupMapping, error) {
	var mapping database.AuthGroupMapping
	query := `SELECT * FROM auth_group_mappings WHERE id = ?`
	if err := s.db.GetContext(ctx, &mapping, s.db.Rebind(query), id); err != nil {
		return nil, fmt.Errorf("getting auth group mapping: %w", err)
	}
	return &mapping, nil
}

func (s *AuthGroupMappingStore) Create(ctx context.Context, mapping *database.AuthGroupMapping) error {
	query := `INSERT INTO auth_group_mappings (auth_source, group_identifier, project_id, role, from_config)
		VALUES (?, ?, ?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		mapping.AuthSource, mapping.GroupIdentifier, mapping.ProjectID, mapping.Role, mapping.FromConfig)
	if err != nil {
		return fmt.Errorf("creating auth group mapping: %w", err)
	}

	id, err := result.LastInsertId()
	if err == nil {
		mapping.ID = id
	}
	return nil
}

func (s *AuthGroupMappingStore) Update(ctx context.Context, mapping *database.AuthGroupMapping) error {
	query := `UPDATE auth_group_mappings SET auth_source = ?, group_identifier = ?, project_id = ?, role = ?, from_config = ?
		WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		mapping.AuthSource, mapping.GroupIdentifier, mapping.ProjectID, mapping.Role, mapping.FromConfig, mapping.ID)
	if err != nil {
		return fmt.Errorf("updating auth group mapping: %w", err)
	}
	return nil
}

func (s *AuthGroupMappingStore) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM auth_group_mappings WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), id)
	if err != nil {
		return fmt.Errorf("deleting auth group mapping: %w", err)
	}
	return nil
}

// SyncFromConfig synchronizes config file mappings with the database.
// It creates or updates mappings from config (marked with from_config=true)
// and removes any config-sourced mappings that are no longer in the config.
func (s *AuthGroupMappingStore) SyncFromConfig(ctx context.Context, source string, mappings []database.AuthGroupMapping) error {
	// Get existing config-sourced mappings
	var existing []database.AuthGroupMapping
	query := `SELECT * FROM auth_group_mappings WHERE auth_source = ? AND from_config = true`
	if s.db.DriverName() == "sqlite" || s.db.DriverName() == "sqlite3" {
		query = `SELECT * FROM auth_group_mappings WHERE auth_source = ? AND from_config = 1`
	}
	if err := s.db.SelectContext(ctx, &existing, s.db.Rebind(query), source); err != nil {
		return fmt.Errorf("listing existing config mappings: %w", err)
	}

	// Build a set of existing mappings for comparison
	existingMap := make(map[string]database.AuthGroupMapping)
	for _, m := range existing {
		key := fmt.Sprintf("%s|%d", m.GroupIdentifier, m.ProjectID)
		existingMap[key] = m
	}

	// Build a set of new mappings
	newMap := make(map[string]database.AuthGroupMapping)
	for _, m := range mappings {
		key := fmt.Sprintf("%s|%d", m.GroupIdentifier, m.ProjectID)
		newMap[key] = m
	}

	// Create or update mappings from config
	for key, m := range newMap {
		m.AuthSource = source
		m.FromConfig = true

		if existingMapping, exists := existingMap[key]; exists {
			// Update if role changed
			if existingMapping.Role != m.Role {
				m.ID = existingMapping.ID
				if err := s.Update(ctx, &m); err != nil {
					return err
				}
			}
		} else {
			// Create new mapping
			if err := s.Create(ctx, &m); err != nil {
				return err
			}
		}
	}

	// Delete mappings that are no longer in config
	for key, m := range existingMap {
		if _, exists := newMap[key]; !exists {
			if err := s.Delete(ctx, m.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

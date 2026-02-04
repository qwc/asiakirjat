package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

type GlobalAccessStore struct {
	db *sqlx.DB
}

func NewGlobalAccessStore(db *sqlx.DB) *GlobalAccessStore {
	return &GlobalAccessStore{db: db}
}

// --- Rules (global_access table) ---

func (s *GlobalAccessStore) ListRules(ctx context.Context) ([]database.GlobalAccess, error) {
	var rules []database.GlobalAccess
	query := `SELECT * FROM global_access ORDER BY subject_type, subject_identifier`
	if err := s.db.SelectContext(ctx, &rules, query); err != nil {
		return nil, fmt.Errorf("listing global access rules: %w", err)
	}
	return rules, nil
}

func (s *GlobalAccessStore) CreateRule(ctx context.Context, rule *database.GlobalAccess) error {
	query := `INSERT INTO global_access (subject_type, subject_identifier, role, from_config) VALUES (?, ?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		rule.SubjectType, rule.SubjectIdentifier, rule.Role, rule.FromConfig)
	if err != nil {
		return fmt.Errorf("creating global access rule: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	rule.ID = id
	return nil
}

func (s *GlobalAccessStore) DeleteRule(ctx context.Context, id int64) error {
	query := `DELETE FROM global_access WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), id)
	if err != nil {
		return fmt.Errorf("deleting global access rule: %w", err)
	}
	return nil
}

// SyncFromConfig replaces all config-sourced rules with the provided set.
func (s *GlobalAccessStore) SyncFromConfig(ctx context.Context, rules []database.GlobalAccess) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing config-sourced rules
	deleteQuery := `DELETE FROM global_access WHERE from_config = 1`
	if _, err := tx.ExecContext(ctx, deleteQuery); err != nil {
		return fmt.Errorf("deleting config rules: %w", err)
	}

	// Insert new config-sourced rules
	insertQuery := tx.Rebind(`INSERT INTO global_access (subject_type, subject_identifier, role, from_config) VALUES (?, ?, ?, 1)`)
	for _, rule := range rules {
		if _, err := tx.ExecContext(ctx, insertQuery, rule.SubjectType, rule.SubjectIdentifier, rule.Role); err != nil {
			// Skip duplicates (may conflict with UI-created rules)
			continue
		}
	}

	return tx.Commit()
}

// --- Grants (global_access_grants table) ---

func (s *GlobalAccessStore) GetGrantByUser(ctx context.Context, userID int64) (*database.GlobalAccessGrant, error) {
	var grant database.GlobalAccessGrant
	// Return the highest-priority grant for this user (any source)
	query := `SELECT * FROM global_access_grants WHERE user_id = ? ORDER BY
		CASE role WHEN 'admin' THEN 3 WHEN 'editor' THEN 2 WHEN 'viewer' THEN 1 ELSE 0 END DESC
		LIMIT 1`
	if err := s.db.GetContext(ctx, &grant, s.db.Rebind(query), userID); err != nil {
		return nil, fmt.Errorf("getting global access grant: %w", err)
	}
	return &grant, nil
}

func (s *GlobalAccessStore) UpsertGrant(ctx context.Context, grant *database.GlobalAccessGrant) error {
	// Try insert, on conflict update role
	query := `INSERT INTO global_access_grants (user_id, role, source) VALUES (?, ?, ?)
		ON CONFLICT(user_id, source) DO UPDATE SET role = excluded.role`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), grant.UserID, grant.Role, grant.Source)
	if err != nil {
		return fmt.Errorf("upserting global access grant: %w", err)
	}
	return nil
}

func (s *GlobalAccessStore) DeleteGrantsBySource(ctx context.Context, userID int64, source string) error {
	query := `DELETE FROM global_access_grants WHERE user_id = ? AND source = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), userID, source)
	if err != nil {
		return fmt.Errorf("deleting global access grants: %w", err)
	}
	return nil
}

func (s *GlobalAccessStore) ListGrants(ctx context.Context) ([]database.GlobalAccessGrant, error) {
	var grants []database.GlobalAccessGrant
	query := `SELECT * FROM global_access_grants ORDER BY user_id`
	if err := s.db.SelectContext(ctx, &grants, query); err != nil {
		return nil, fmt.Errorf("listing global access grants: %w", err)
	}
	return grants, nil
}

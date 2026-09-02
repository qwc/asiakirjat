package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
)

// AccessGrantStore manages the grant edge: a group or a single user pointed at
// an org or a project, with a role (issues #150, #151).
type AccessGrantStore struct {
	db *sqlx.DB
}

func NewAccessGrantStore(db *sqlx.DB) *AccessGrantStore {
	return &AccessGrantStore{db: db}
}

// Grant creates or updates one grant. The unique indexes make a repeat grant
// for the same subject and scope an update of its role rather than a second
// row, so the UI's "grant" button is idempotent.
func (s *AccessGrantStore) Grant(ctx context.Context, g *database.AccessGrant) error {
	if g.Source == "" {
		g.Source = database.GrantSourceManual
	}
	if !g.Valid() {
		return fmt.Errorf("invalid access grant: needs exactly one subject, one scope, a known role and source")
	}

	existing, err := s.find(ctx, g)
	if err != nil {
		return err
	}
	if existing != nil {
		query := `UPDATE access_grants SET role = ?, source = ? WHERE id = ?`
		if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), g.Role, g.Source, existing.ID); err != nil {
			return fmt.Errorf("updating access grant: %w", err)
		}
		g.ID = existing.ID
		return nil
	}

	query := `INSERT INTO access_grants (group_id, user_id, org_id, project_id, role, source)
		VALUES (?, ?, ?, ?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		g.GroupID, g.UserID, g.OrgID, g.ProjectID, g.Role, g.Source)
	if err != nil {
		return fmt.Errorf("creating access grant: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	g.ID = id
	return nil
}

// find locates an existing grant for the same subject and scope. NULL columns
// need IS NULL rather than = ?, so the predicate is assembled per shape.
func (s *AccessGrantStore) find(ctx context.Context, g *database.AccessGrant) (*database.AccessGrant, error) {
	query := `SELECT * FROM access_grants WHERE `
	var args []any

	if g.GroupID != nil {
		query += `group_id = ? AND user_id IS NULL AND `
		args = append(args, *g.GroupID)
	} else {
		query += `user_id = ? AND group_id IS NULL AND `
		args = append(args, *g.UserID)
	}
	if g.OrgID != nil {
		query += `org_id = ? AND project_id IS NULL`
		args = append(args, *g.OrgID)
	} else {
		query += `project_id = ? AND org_id IS NULL`
		args = append(args, *g.ProjectID)
	}

	var grants []database.AccessGrant
	if err := s.db.SelectContext(ctx, &grants, s.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("looking up access grant: %w", err)
	}
	if len(grants) == 0 {
		return nil, nil
	}
	return &grants[0], nil
}

// Revoke deletes one grant by id and reports whether a row actually went. A
// revoke that silently matched nothing is the failure shape issue #126 was:
// the page redirects, and the access is still there.
func (s *AccessGrantStore) Revoke(ctx context.Context, id int64) (bool, error) {
	query := `DELETE FROM access_grants WHERE id = ?`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query), id)
	if err != nil {
		return false, fmt.Errorf("revoking access grant: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("checking revoked rows: %w", err)
	}
	return affected > 0, nil
}

func (s *AccessGrantStore) ListByProject(ctx context.Context, projectID int64) ([]database.AccessGrant, error) {
	var grants []database.AccessGrant
	query := `SELECT * FROM access_grants WHERE project_id = ? ORDER BY id`
	if err := s.db.SelectContext(ctx, &grants, s.db.Rebind(query), projectID); err != nil {
		return nil, fmt.Errorf("listing grants for project: %w", err)
	}
	return grants, nil
}

func (s *AccessGrantStore) ListByOrg(ctx context.Context, orgID int64) ([]database.AccessGrant, error) {
	var grants []database.AccessGrant
	query := `SELECT * FROM access_grants WHERE org_id = ? ORDER BY id`
	if err := s.db.SelectContext(ctx, &grants, s.db.Rebind(query), orgID); err != nil {
		return nil, fmt.Errorf("listing grants for org: %w", err)
	}
	return grants, nil
}

// ListByUser returns the grants held by one user directly — not the ones they
// reach through a group. The robots page shows these, because a robot's reach
// is now a set of rows an admin can read and revoke rather than an instance
// role nobody can see (#155).
func (s *AccessGrantStore) ListByUser(ctx context.Context, userID int64) ([]database.AccessGrant, error) {
	var grants []database.AccessGrant
	query := `SELECT * FROM access_grants WHERE user_id = ? ORDER BY id`
	if err := s.db.SelectContext(ctx, &grants, s.db.Rebind(query), userID); err != nil {
		return nil, fmt.Errorf("listing grants for user: %w", err)
	}
	return grants, nil
}

// DeleteBySource removes every grant owned by one source. config-sourced rows
// are owned by config.yaml and replaced wholesale on each startup sync;
// manual rows are never touched by it.
func (s *AccessGrantStore) DeleteBySource(ctx context.Context, source string) error {
	query := `DELETE FROM access_grants WHERE source = ?`
	if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), source); err != nil {
		return fmt.Errorf("deleting grants by source: %w", err)
	}
	return nil
}

// GrantsForUser returns every role a user holds, keyed by scope: one map for
// projects granted directly and one for orgs. The checker combines them —
// an org role cascades to every project in that org.
//
// This is the one query the whole model rests on. It resolves, in a single
// round-trip:
//
//   - grants naming the user directly;
//   - grants naming a group the user is in, either because a member names
//     their username, or because the login sync resolved them into it from an
//     LDAP/OAuth2 group.
//
// Username matching is case-insensitive and happens at check time, which is
// what makes naming a user in a group work immediately instead of waiting for
// a login that may never come (the bug behind issue #135).
func (s *AccessGrantStore) GrantsForUser(ctx context.Context, userID int64, username string) (store.UserGrants, error) {
	result := store.UserGrants{
		Projects: map[int64]string{},
		Orgs:     map[int64]string{},
	}

	type row struct {
		OrgID     *int64 `db:"org_id"`
		ProjectID *int64 `db:"project_id"`
		Role      string `db:"role"`
	}

	query := `SELECT org_id, project_id, role FROM access_grants
		WHERE user_id = ?
		   OR group_id IN (
		        SELECT group_id FROM access_group_members
		         WHERE subject_type = 'user' AND LOWER(subject_identifier) = LOWER(?)
		        UNION
		        SELECT group_id FROM access_group_resolved WHERE user_id = ?
		      )`

	var rows []row
	if err := s.db.SelectContext(ctx, &rows, s.db.Rebind(query), userID, username, userID); err != nil {
		return result, fmt.Errorf("listing grants for user: %w", err)
	}

	for _, r := range rows {
		switch {
		case r.ProjectID != nil:
			if database.GrantRoleRank(r.Role) > database.GrantRoleRank(result.Projects[*r.ProjectID]) {
				result.Projects[*r.ProjectID] = r.Role
			}
		case r.OrgID != nil:
			if database.GrantRoleRank(r.Role) > database.GrantRoleRank(result.Orgs[*r.OrgID]) {
				result.Orgs[*r.OrgID] = r.Role
			}
		}
	}
	return result, nil
}

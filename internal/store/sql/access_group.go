package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

// AccessGroupStore manages access groups and their membership (issues #150,
// #151). A group is a named set of subjects — users, LDAP groups, OAuth2
// groups, or a mix. It confers nothing on its own; an AccessGrant does that.
//
// Members carry no role, deliberately: the role belongs to the grant, so one
// group can be editor on one project and viewer on another.
type AccessGroupStore struct {
	db *sqlx.DB
}

func NewAccessGroupStore(db *sqlx.DB) *AccessGroupStore {
	return &AccessGroupStore{db: db}
}

func (s *AccessGroupStore) List(ctx context.Context) ([]database.AccessGroup, error) {
	var groups []database.AccessGroup
	query := `SELECT * FROM access_groups ORDER BY name`
	if err := s.db.SelectContext(ctx, &groups, query); err != nil {
		return nil, fmt.Errorf("listing access groups: %w", err)
	}
	return groups, nil
}

func (s *AccessGroupStore) GetByID(ctx context.Context, id int64) (*database.AccessGroup, error) {
	var group database.AccessGroup
	query := `SELECT * FROM access_groups WHERE id = ?`
	if err := s.db.GetContext(ctx, &group, s.db.Rebind(query), id); err != nil {
		return nil, fmt.Errorf("getting access group: %w", err)
	}
	return &group, nil
}

func (s *AccessGroupStore) GetByName(ctx context.Context, name string) (*database.AccessGroup, error) {
	var group database.AccessGroup
	query := `SELECT * FROM access_groups WHERE name = ?`
	if err := s.db.GetContext(ctx, &group, s.db.Rebind(query), name); err != nil {
		return nil, fmt.Errorf("getting access group by name: %w", err)
	}
	return &group, nil
}

func (s *AccessGroupStore) Create(ctx context.Context, group *database.AccessGroup) error {
	query := `INSERT INTO access_groups (name, description) VALUES (?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query), group.Name, group.Description)
	if err != nil {
		return fmt.Errorf("creating access group: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	group.ID = id
	return nil
}

func (s *AccessGroupStore) Update(ctx context.Context, group *database.AccessGroup) error {
	query := `UPDATE access_groups SET name = ?, description = ? WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), group.Name, group.Description, group.ID); err != nil {
		return fmt.Errorf("updating access group: %w", err)
	}
	return nil
}

// Delete removes a group. Its members, its resolved memberships and every
// grant naming it go with it via ON DELETE CASCADE — a group that still
// granted access after deletion is exactly the orphan the FKs exist to
// prevent. CountGrants lets the UI say what will be revoked first.
func (s *AccessGroupStore) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM access_groups WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), id); err != nil {
		return fmt.Errorf("deleting access group: %w", err)
	}
	return nil
}

// CountGrants reports how many orgs and projects a group currently grants
// access to, so deleting it can warn rather than silently revoke.
func (s *AccessGroupStore) CountGrants(ctx context.Context, id int64) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM access_grants WHERE group_id = ?`
	if err := s.db.GetContext(ctx, &count, s.db.Rebind(query), id); err != nil {
		return 0, fmt.Errorf("counting grants for access group: %w", err)
	}
	return count, nil
}

func (s *AccessGroupStore) ListMembers(ctx context.Context, groupID int64) ([]database.AccessGroupMember, error) {
	var members []database.AccessGroupMember
	query := `SELECT * FROM access_group_members WHERE group_id = ? ORDER BY subject_type, subject_identifier`
	if err := s.db.SelectContext(ctx, &members, s.db.Rebind(query), groupID); err != nil {
		return nil, fmt.Errorf("listing access group members: %w", err)
	}
	return members, nil
}

// AddMember validates the subject kind at store entry rather than trusting the
// caller — the same rule audit L-1/L-7 established for the tables this
// replaces.
func (s *AccessGroupStore) AddMember(ctx context.Context, m *database.AccessGroupMember) error {
	if !database.ValidSubjectType(m.SubjectType) {
		return fmt.Errorf("invalid subject type %q", m.SubjectType)
	}
	if m.SubjectIdentifier == "" {
		return fmt.Errorf("subject identifier must not be empty")
	}
	query := `INSERT INTO access_group_members (group_id, subject_type, subject_identifier) VALUES (?, ?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query), m.GroupID, m.SubjectType, m.SubjectIdentifier)
	if err != nil {
		return fmt.Errorf("adding access group member: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	m.ID = id
	return nil
}

func (s *AccessGroupStore) RemoveMember(ctx context.Context, memberID int64) error {
	query := `DELETE FROM access_group_members WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), memberID); err != nil {
		return fmt.Errorf("removing access group member: %w", err)
	}
	return nil
}

// ListGroupsBySubject returns the ids of groups naming this auth-provider
// group, used by the login sync to work out what a user's memberships resolve
// to. Matching is case-insensitive, as LDAP DNs and OAuth2 group names arrive
// with inconsistent casing.
func (s *AccessGroupStore) ListGroupsBySubject(ctx context.Context, subjectType, identifier string) ([]int64, error) {
	var ids []int64
	query := `SELECT group_id FROM access_group_members
		WHERE subject_type = ? AND LOWER(subject_identifier) = LOWER(?)`
	if err := s.db.SelectContext(ctx, &ids, s.db.Rebind(query), subjectType, identifier); err != nil {
		return nil, fmt.Errorf("listing groups by subject: %w", err)
	}
	return ids, nil
}

// ListResolvedForUser returns the group ids a user was resolved into by a
// given auth source at login.
func (s *AccessGroupStore) ListResolvedForUser(ctx context.Context, userID int64, source string) ([]int64, error) {
	var ids []int64
	query := `SELECT group_id FROM access_group_resolved WHERE user_id = ? AND source = ?`
	if err := s.db.SelectContext(ctx, &ids, s.db.Rebind(query), userID, source); err != nil {
		return nil, fmt.Errorf("listing resolved groups for user: %w", err)
	}
	return ids, nil
}

// SetResolvedForUser reconciles which groups a user belongs to according to one
// auth source, adding what is new and removing what is gone.
//
// It reconciles rather than deleting and re-inserting on purpose: a
// delete-then-insert leaves a window during login where a still-valid user
// holds no membership, and a crash in that window locks them out until their
// next successful sign-in.
func (s *AccessGroupStore) SetResolvedForUser(ctx context.Context, userID int64, source string, groupIDs []int64) error {
	existing, err := s.ListResolvedForUser(ctx, userID, source)
	if err != nil {
		return err
	}

	want := make(map[int64]bool, len(groupIDs))
	for _, id := range groupIDs {
		want[id] = true
	}
	have := make(map[int64]bool, len(existing))
	for _, id := range existing {
		have[id] = true
	}

	for id := range want {
		if have[id] {
			continue
		}
		query := `INSERT INTO access_group_resolved (group_id, user_id, source) VALUES (?, ?, ?)`
		if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), id, userID, source); err != nil {
			return fmt.Errorf("recording resolved group membership: %w", err)
		}
	}
	for id := range have {
		if want[id] {
			continue
		}
		query := `DELETE FROM access_group_resolved WHERE group_id = ? AND user_id = ? AND source = ?`
		if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), id, userID, source); err != nil {
			return fmt.Errorf("removing resolved group membership: %w", err)
		}
	}
	return nil
}

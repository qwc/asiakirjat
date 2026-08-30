package sql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

// AccessListStore manages named access lists and their members (issue #125).
// A list is a reusable set of subjects — LDAP groups, OAuth2 groups, named
// users, or a mix — that a project can point at instead of repeating
// per-project grants.
type AccessListStore struct {
	db *sqlx.DB
}

func NewAccessListStore(db *sqlx.DB) *AccessListStore {
	return &AccessListStore{db: db}
}

func (s *AccessListStore) List(ctx context.Context) ([]database.AccessList, error) {
	var lists []database.AccessList
	query := `SELECT * FROM access_lists ORDER BY name`
	if err := s.db.SelectContext(ctx, &lists, query); err != nil {
		return nil, fmt.Errorf("listing access lists: %w", err)
	}
	return lists, nil
}

func (s *AccessListStore) GetByID(ctx context.Context, id int64) (*database.AccessList, error) {
	var list database.AccessList
	query := `SELECT * FROM access_lists WHERE id = ?`
	if err := s.db.GetContext(ctx, &list, s.db.Rebind(query), id); err != nil {
		return nil, fmt.Errorf("getting access list: %w", err)
	}
	return &list, nil
}

func (s *AccessListStore) GetByName(ctx context.Context, name string) (*database.AccessList, error) {
	var list database.AccessList
	query := `SELECT * FROM access_lists WHERE name = ?`
	if err := s.db.GetContext(ctx, &list, s.db.Rebind(query), name); err != nil {
		return nil, fmt.Errorf("getting access list by name: %w", err)
	}
	return &list, nil
}

func (s *AccessListStore) Create(ctx context.Context, list *database.AccessList) error {
	query := `INSERT INTO access_lists (name, description) VALUES (?, ?)`
	result, err := s.db.ExecContext(ctx, s.db.Rebind(query), list.Name, list.Description)
	if err != nil {
		return fmt.Errorf("creating access list: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("getting last insert id: %w", err)
	}
	list.ID = id
	return nil
}

func (s *AccessListStore) Update(ctx context.Context, list *database.AccessList) error {
	query := `UPDATE access_lists SET name = ?, description = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query), list.Name, list.Description, list.ID)
	if err != nil {
		return fmt.Errorf("updating access list: %w", err)
	}
	return nil
}

// Delete removes a list and, by cascade, its members. It fails while any
// project still points at the list: the FK on projects.access_list_id is
// ON DELETE RESTRICT, so a list in use cannot silently stop governing the
// projects that named it.
func (s *AccessListStore) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM access_lists WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), id); err != nil {
		return fmt.Errorf("deleting access list: %w", err)
	}
	return nil
}

// CountProjectsUsing reports how many projects point at this list, so callers
// can explain a refused delete instead of surfacing a constraint error.
func (s *AccessListStore) CountProjectsUsing(ctx context.Context, id int64) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM projects WHERE access_list_id = ?`
	if err := s.db.GetContext(ctx, &count, s.db.Rebind(query), id); err != nil {
		return 0, fmt.Errorf("counting projects using access list: %w", err)
	}
	return count, nil
}

// --- Members ---

func (s *AccessListStore) ListMembers(ctx context.Context, listID int64) ([]database.AccessListMember, error) {
	var members []database.AccessListMember
	query := `SELECT * FROM access_list_members WHERE list_id = ?
		ORDER BY subject_type, subject_identifier`
	if err := s.db.SelectContext(ctx, &members, s.db.Rebind(query), listID); err != nil {
		return nil, fmt.Errorf("listing access list members: %w", err)
	}
	return members, nil
}

// AddMember inserts a subject into a list, or updates the role if that
// subject is already a member.
func (s *AccessListStore) AddMember(ctx context.Context, m *database.AccessListMember) error {
	if !database.ValidSubjectType(m.SubjectType) {
		return fmt.Errorf("invalid subject type %q", m.SubjectType)
	}
	if !database.ValidAccessRole(m.Role) {
		return fmt.Errorf("invalid access role %q", m.Role)
	}

	var query string
	if s.db.DriverName() == "mysql" {
		query = `INSERT INTO access_list_members (list_id, subject_type, subject_identifier, role)
			VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE role = ?`
	} else {
		query = `INSERT INTO access_list_members (list_id, subject_type, subject_identifier, role)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(list_id, subject_type, subject_identifier) DO UPDATE SET role = ?`
	}
	_, err := s.db.ExecContext(ctx, s.db.Rebind(query),
		m.ListID, m.SubjectType, m.SubjectIdentifier, m.Role, m.Role)
	if err != nil {
		return fmt.Errorf("adding access list member: %w", err)
	}
	return nil
}

func (s *AccessListStore) RemoveMember(ctx context.Context, memberID int64) error {
	query := `DELETE FROM access_list_members WHERE id = ?`
	if _, err := s.db.ExecContext(ctx, s.db.Rebind(query), memberID); err != nil {
		return fmt.Errorf("removing access list member: %w", err)
	}
	return nil
}

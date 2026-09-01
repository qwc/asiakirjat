package store

import (
	"context"

	"github.com/qwc/asiakirjat/internal/database"
)

type ProjectStore interface {
	Create(ctx context.Context, project *database.Project) error
	GetBySlug(ctx context.Context, slug string) (*database.Project, error)
	GetByID(ctx context.Context, id int64) (*database.Project, error)
	List(ctx context.Context) ([]database.Project, error)
	ListByVisibility(ctx context.Context, visibility string) ([]database.Project, error)
	Search(ctx context.Context, query string) ([]database.Project, error)
	Update(ctx context.Context, project *database.Project) error
	Delete(ctx context.Context, id int64) error
}

type VersionStore interface {
	Create(ctx context.Context, version *database.Version) error
	GetByProjectAndTag(ctx context.Context, projectID int64, tag string) (*database.Version, error)
	ListByProject(ctx context.Context, projectID int64) ([]database.Version, error)
	Update(ctx context.Context, version *database.Version) error
	Delete(ctx context.Context, id int64) error
}

type UserStore interface {
	Create(ctx context.Context, user *database.User) error
	GetByID(ctx context.Context, id int64) (*database.User, error)
	GetByUsername(ctx context.Context, username string) (*database.User, error)
	List(ctx context.Context) ([]database.User, error)
	ListRobots(ctx context.Context) ([]database.User, error)
	Update(ctx context.Context, user *database.User) error
	Delete(ctx context.Context, id int64) error
	Count(ctx context.Context) (int64, error)
}

type SessionStore interface {
	Create(ctx context.Context, session *database.Session) error
	GetByID(ctx context.Context, id string) (*database.Session, error)
	Delete(ctx context.Context, id string) error
	DeleteExpired(ctx context.Context) error
}

type ProjectAccessStore interface {
	Grant(ctx context.Context, access *database.ProjectAccess) error
	Revoke(ctx context.Context, projectID, userID int64) error
	RevokeBySource(ctx context.Context, projectID, userID int64, source string) error
	RevokeProjectBySource(ctx context.Context, projectID int64, source string) error
	RevokeManualEditorByUser(ctx context.Context, userID int64) error
	GetAccess(ctx context.Context, projectID, userID int64) (*database.ProjectAccess, error)
	GetAccessBySource(ctx context.Context, projectID, userID int64, source string) (*database.ProjectAccess, error)
	ListByProject(ctx context.Context, projectID int64) ([]database.ProjectAccess, error)
	ListByUser(ctx context.Context, userID int64) ([]database.ProjectAccess, error)
	ListByUserAndSource(ctx context.Context, userID int64, source string) ([]database.ProjectAccess, error)
	ListAccessibleProjectIDs(ctx context.Context, userID int64) ([]int64, error)
	GetEffectiveRole(ctx context.Context, projectID, userID int64) (string, error)
}

type AuthGroupMappingStore interface {
	List(ctx context.Context) ([]database.AuthGroupMapping, error)
	ListBySource(ctx context.Context, source string) ([]database.AuthGroupMapping, error)
	GetByID(ctx context.Context, id int64) (*database.AuthGroupMapping, error)
	Create(ctx context.Context, mapping *database.AuthGroupMapping) error
	Update(ctx context.Context, mapping *database.AuthGroupMapping) error
	Delete(ctx context.Context, id int64) error
	SyncFromConfig(ctx context.Context, source string, mappings []database.AuthGroupMapping) error
}

type TokenStore interface {
	Create(ctx context.Context, token *database.APIToken) error
	GetByID(ctx context.Context, id int64) (*database.APIToken, error)
	GetByHash(ctx context.Context, hash string) (*database.APIToken, error)
	ListByUser(ctx context.Context, userID int64) ([]database.APIToken, error)
	ListByProject(ctx context.Context, projectID int64) ([]database.APIToken, error)
	Delete(ctx context.Context, id int64) error
}

type UploadLogStore interface {
	Create(ctx context.Context, log *database.UploadLog) error
	ListByProject(ctx context.Context, projectID int64) ([]database.UploadLog, error)
}

// AccessListStore manages named access lists: reusable sets of subjects that
// a project can point at via Visibility = VisibilityList (issue #125).
type AccessListStore interface {
	List(ctx context.Context) ([]database.AccessList, error)
	GetByID(ctx context.Context, id int64) (*database.AccessList, error)
	GetByName(ctx context.Context, name string) (*database.AccessList, error)
	Create(ctx context.Context, list *database.AccessList) error
	Update(ctx context.Context, list *database.AccessList) error
	Delete(ctx context.Context, id int64) error
	CountProjectsUsing(ctx context.Context, id int64) (int, error)

	ListMembers(ctx context.Context, listID int64) ([]database.AccessListMember, error)
	AddMember(ctx context.Context, m *database.AccessListMember) error
	RemoveMember(ctx context.Context, memberID int64) error

	ListMembersBySubjectType(ctx context.Context, subjectType string) ([]database.AccessListMember, error)

	UpsertGrant(ctx context.Context, g *database.AccessListGrant) error
	ListGrantsByUserAndSource(ctx context.Context, userID int64, source string) ([]database.AccessListGrant, error)
	DeleteGrant(ctx context.Context, listID, userID int64, source string) error
	DeleteGrantsBySource(ctx context.Context, userID int64, source string) error
	RolesForUser(ctx context.Context, userID int64, username string) (map[int64]string, error)
}

type GlobalAccessStore interface {
	// Rules (global_access table)
	ListRules(ctx context.Context) ([]database.GlobalAccess, error)
	CreateRule(ctx context.Context, rule *database.GlobalAccess) error
	GetUserRule(ctx context.Context, username string) (*database.GlobalAccess, error)
	DeleteRule(ctx context.Context, id int64) error
	SyncFromConfig(ctx context.Context, rules []database.GlobalAccess) error

	// Grants (global_access_grants table — resolved per-user)
	GetGrantByUser(ctx context.Context, userID int64) (*database.GlobalAccessGrant, error)
	UpsertGrant(ctx context.Context, grant *database.GlobalAccessGrant) error
	DeleteGrantsBySource(ctx context.Context, userID int64, source string) error
	ListGrants(ctx context.Context) ([]database.GlobalAccessGrant, error)
}

// ---------------------------------------------------------------------------
// Unified access model (issues #150, #151)
// ---------------------------------------------------------------------------

// OrgStore manages organizations, the container above projects. Every project
// belongs to exactly one; installations predating orgs get the 'default' org.
type OrgStore interface {
	List(ctx context.Context) ([]database.Org, error)
	GetByID(ctx context.Context, id int64) (*database.Org, error)
	GetBySlug(ctx context.Context, slug string) (*database.Org, error)
	Create(ctx context.Context, org *database.Org) error
	Update(ctx context.Context, org *database.Org) error
	Delete(ctx context.Context, id int64) error
	CountProjects(ctx context.Context, id int64) (int, error)
}

// AccessGroupStore manages access groups and their membership. Members carry
// no role: the role belongs to the grant.
type AccessGroupStore interface {
	List(ctx context.Context) ([]database.AccessGroup, error)
	GetByID(ctx context.Context, id int64) (*database.AccessGroup, error)
	GetByName(ctx context.Context, name string) (*database.AccessGroup, error)
	Create(ctx context.Context, group *database.AccessGroup) error
	Update(ctx context.Context, group *database.AccessGroup) error
	Delete(ctx context.Context, id int64) error
	CountGrants(ctx context.Context, id int64) (int, error)

	ListMembers(ctx context.Context, groupID int64) ([]database.AccessGroupMember, error)
	AddMember(ctx context.Context, m *database.AccessGroupMember) error
	RemoveMember(ctx context.Context, memberID int64) error

	ListGroupsBySubject(ctx context.Context, subjectType, identifier string) ([]int64, error)
	ListResolvedForUser(ctx context.Context, userID int64, source string) ([]int64, error)
	SetResolvedForUser(ctx context.Context, userID int64, source string, groupIDs []int64) error
}

// UserGrants is one user's roles, keyed by the scope that granted them. The
// checker combines the two: an org role applies to every project in that org,
// and the strongest role from either wins.
type UserGrants struct {
	Projects map[int64]string
	Orgs     map[int64]string
}

// AccessGrantStore manages the grant edge: group-or-user to org-or-project,
// with a role.
type AccessGrantStore interface {
	Grant(ctx context.Context, g *database.AccessGrant) error
	Revoke(ctx context.Context, id int64) (bool, error)
	ListByProject(ctx context.Context, projectID int64) ([]database.AccessGrant, error)
	ListByOrg(ctx context.Context, orgID int64) ([]database.AccessGrant, error)
	DeleteBySource(ctx context.Context, source string) error
	GrantsForUser(ctx context.Context, userID int64, username string) (UserGrants, error)
}

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
	ListPublic(ctx context.Context) ([]database.Project, error)
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

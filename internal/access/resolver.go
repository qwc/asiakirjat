package access

import (
	"context"
	"log/slog"
	"sort"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
)

// Resolver answers the same questions as Checker, against the unified access
// model (issues #150, #151): access groups, the grant edge, and a project's
// exposure.
//
// The whole policy is one sentence — your role on a project is the strongest
// role any grant gives you, on the project or on its org — which is why this
// file is a fraction of the size of the four-mechanism version it replaces.
//
// Two instance-level rules survive from the old model, unchanged on purpose so
// the migration can be shown to preserve access exactly:
//
//   - users.role == "admin" is admin everywhere.
//   - users.role == "editor" may upload anywhere, including to projects it
//     cannot view. The audit flagged that asymmetry as M-2; it is preserved
//     here rather than quietly fixed, because changing it is a product
//     decision and not part of this migration.
type Resolver struct {
	grants store.AccessGrantStore
	logger *slog.Logger
}

func NewResolver(grants store.AccessGrantStore, logger *slog.Logger) *Resolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &Resolver{grants: grants, logger: logger}
}

// RoleFor returns the strongest role a user holds on a project through grants
// alone, or "" for none. It deliberately ignores users.role: instance-level
// powers are applied by the callers below, so that "what did a grant give this
// person" stays answerable on its own — which is what the admin UI needs to
// show.
func (r *Resolver) RoleFor(ctx context.Context, user *database.User, project *database.Project) string {
	if user == nil {
		return ""
	}
	held, err := r.grants.GrantsForUser(ctx, user.ID, user.Username)
	if err != nil {
		r.logger.Error("resolving grants for user", "username", user.Username, "error", err)
		return ""
	}
	return roleFromGrants(held, project)
}

// roleFromGrants combines the project-scoped and org-scoped roles a user
// holds. An org grant cascades to every project in the org; the stronger of
// the two wins.
func roleFromGrants(held store.UserGrants, project *database.Project) string {
	role := held.Projects[project.ID]
	if project.OrgID != nil {
		if orgRole := held.Orgs[*project.OrgID]; database.GrantRoleRank(orgRole) > database.GrantRoleRank(role) {
			role = orgRole
		}
	}
	return role
}

// CanView reports whether user may read project.
func (r *Resolver) CanView(ctx context.Context, user *database.User, project *database.Project) bool {
	if project.Exposure == database.ExposurePublic {
		return true
	}
	if user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	if project.Exposure == database.ExposureAuthenticated {
		return true
	}
	return r.RoleFor(ctx, user, project) != ""
}

// CanUpload reports whether user may add or replace versions of project.
func (r *Resolver) CanUpload(ctx context.Context, user *database.User, project *database.Project) bool {
	if user == nil {
		return false
	}
	if user.Role == "admin" || user.Role == "editor" {
		return true
	}
	return database.GrantRoleRank(r.RoleFor(ctx, user, project)) >= database.GrantRoleRank(database.GrantRoleEditor)
}

// OrgsWhereCanCreate returns the organizations in which user may create
// projects: an editor or admin grant on an org is what "may add projects
// here" means now (#155). A robot with a global token used to be able to
// create anywhere because every robot was an instance editor; its reach is a
// grant like anyone else's, and this is where that reach is read.
//
// Instance admins and editors are not listed — they may create anywhere, and
// callers check that first.
func (r *Resolver) OrgsWhereCanCreate(ctx context.Context, user *database.User) []int64 {
	if user == nil {
		return nil
	}
	held, err := r.grants.GrantsForUser(ctx, user.ID, user.Username)
	if err != nil {
		r.logger.Error("resolving grants for user", "username", user.Username, "error", err)
		return nil
	}

	var orgs []int64
	for orgID, role := range held.Orgs {
		if database.GrantRoleRank(role) >= database.GrantRoleRank(database.GrantRoleEditor) {
			orgs = append(orgs, orgID)
		}
	}
	sort.Slice(orgs, func(i, j int) bool { return orgs[i] < orgs[j] })
	return orgs
}

// CanManage reports whether user may administer project: its settings and who
// may reach it.
//
// Project ownership is no longer a special case. PR #118 decided this from
// projects.created_by; the migration turned every creator into an admin grant
// on their project, so "the creator can manage it" and "an org admin can
// manage it" are now the same rule instead of two.
func (r *Resolver) CanManage(ctx context.Context, user *database.User, project *database.Project) bool {
	if user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	return r.RoleFor(ctx, user, project) == database.GrantRoleAdmin
}

// FilterAccessible returns the subset of all that user may view, using one
// grant lookup for the whole list rather than one per project.
func (r *Resolver) FilterAccessible(ctx context.Context, user *database.User, all []database.Project) []database.Project {
	var filtered []database.Project

	if user == nil {
		for _, p := range all {
			if p.Exposure == database.ExposurePublic {
				filtered = append(filtered, p)
			}
		}
		return filtered
	}

	held, err := r.grants.GrantsForUser(ctx, user.ID, user.Username)
	if err != nil {
		r.logger.Error("resolving grants for user", "username", user.Username, "error", err)
		held = store.UserGrants{Projects: map[int64]string{}, Orgs: map[int64]string{}}
	}

	for i := range all {
		p := all[i]
		switch {
		case p.Exposure == database.ExposurePublic,
			user.Role == "admin",
			p.Exposure == database.ExposureAuthenticated,
			roleFromGrants(held, &p) != "":
			filtered = append(filtered, p)
		}
	}
	return filtered
}

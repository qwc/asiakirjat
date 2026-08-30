// Package access centralizes the authorization decisions that previously
// lived as methods on handler.Handler (canViewProject, canUpload,
// filterAccessibleProjects). Moving them here makes the model testable in
// isolation and gives us one place to evolve the rules instead of three
// drift-prone copies — the original bug that motivated this refactor.
//
// The package depends only on the two stores it consults (ProjectAccess,
// GlobalAccess) plus a logger; it has no knowledge of HTTP, handlers, or
// the surrounding wiring.
package access

import (
	"context"
	"log/slog"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
)

// Checker performs read/upload access decisions for users against projects.
// Construct via NewChecker.
type Checker struct {
	access       store.ProjectAccessStore
	globalAccess store.GlobalAccessStore
	accessLists  store.AccessListStore
	logger       *slog.Logger
}

// NewChecker wires the stores. logger may be nil; slog's default is used.
// globalAccess and accessLists may be nil in tests / deployments without
// those features; the checker treats a nil store as "no grants" everywhere.
func NewChecker(a store.ProjectAccessStore, g store.GlobalAccessStore, al store.AccessListStore, l *slog.Logger) *Checker {
	if l == nil {
		l = slog.Default()
	}
	return &Checker{access: a, globalAccess: g, accessLists: al, logger: l}
}

// globalRole returns the user's effective global-access role for
// private-visibility projects, or "" if they have none.
//
// Two things can confer it, and the stronger of the two wins:
//
//   - A resolved grant, written by the LDAP/OAuth2 login sync from a
//     matching ldap_group / oauth2_group rule.
//   - A rule naming the user directly (subject_type 'user'), matched here
//     at check time. Nothing ever resolved those into grants — main.go's
//     config sync had a loop over them whose body was `continue` — so
//     naming a user in Admin > Global Access or access.private.*.users
//     silently granted nothing.
func (c *Checker) globalRole(ctx context.Context, user *database.User) string {
	if c.globalAccess == nil || user == nil {
		return ""
	}
	var role string
	if grant, err := c.globalAccess.GetGrantByUser(ctx, user.ID); err == nil && grant != nil {
		role = grant.Role
	}
	if rule, err := c.globalAccess.GetUserRule(ctx, user.Username); err == nil && rule != nil {
		if roleRank(rule.Role) > roleRank(role) {
			role = rule.Role
		}
	}
	return role
}

// listRole returns the user's role in the named access list governing this
// project, or "" if the list does not admit them. A project whose visibility
// is VisibilityList but whose list pointer is missing admits nobody: the list
// it named is the only thing that could let anyone in, so it fails closed.
func (c *Checker) listRole(ctx context.Context, user *database.User, project *database.Project) string {
	if c.accessLists == nil || user == nil || project.AccessListID == nil {
		return ""
	}
	roles, err := c.accessLists.RolesForUser(ctx, user.ID, user.Username)
	if err != nil {
		c.logger.Debug("access list lookup failed", "username", user.Username, "project", project.Slug, "error", err)
		return ""
	}
	return roles[*project.AccessListID]
}

// roleRank orders access roles so the strongest of several sources wins.
func roleRank(role string) int {
	switch role {
	case "admin":
		return 3
	case "editor":
		return 2
	case "viewer":
		return 1
	default:
		return 0
	}
}

// CanView reports whether user is allowed to view project.
//
// Rules:
//   - VisibilityPublic: always true.
//   - Anonymous (user == nil) on non-public: false.
//   - Global role admin: true.
//   - VisibilityPrivate: true if a GlobalAccessGrant exists for user, OR
//     a per-project ProjectAccess grant exists. The latter is what makes
//     the Service.Create auto-grant rule actually work for the default
//     `private` visibility (audit M-14).
//   - VisibilityCustom: true iff ProjectAccess.GetEffectiveRole returns
//     a non-empty role for (project, user). No global-access fast path,
//     so custom is strictly more restrictive than private.
func (c *Checker) CanView(ctx context.Context, user *database.User, project *database.Project) bool {
	username := "<anonymous>"
	if user != nil {
		username = user.Username
	}
	if project.Visibility == database.VisibilityPublic {
		return true
	}
	if user == nil {
		c.logger.Debug("access denied: anonymous user, non-public project", "project", project.Slug, "visibility", project.Visibility)
		return false
	}
	if user.Role == "admin" {
		c.logger.Debug("access granted: admin user", "username", username, "project", project.Slug)
		return true
	}
	if project.Visibility == database.VisibilityPrivate {
		if role := c.globalRole(ctx, user); role != "" {
			c.logger.Debug("access granted: global access", "username", username, "project", project.Slug, "global_role", role)
			return true
		}
		// Fall through to per-project grant. Same store call as the
		// custom branch below; kept separate so the debug log identifies
		// which gate let the user in.
		effectiveRole, err := c.access.GetEffectiveRole(ctx, project.ID, user.ID)
		if err == nil && effectiveRole != "" {
			c.logger.Debug("access granted: per-project grant on private", "username", username, "project", project.Slug, "effective_role", effectiveRole)
			return true
		}
		c.logger.Debug("access denied: no global or per-project grant for private project", "username", username, "project", project.Slug, "user_id", user.ID)
		return false
	}
	if project.Visibility == database.VisibilityList {
		if role := c.listRole(ctx, user, project); role != "" {
			c.logger.Debug("access granted: access list", "username", username, "project", project.Slug, "list_role", role)
			return true
		}
		// A per-project grant still counts, matching the private branch:
		// naming someone on the project itself is more specific than the
		// list, never less.
		effectiveRole, err := c.access.GetEffectiveRole(ctx, project.ID, user.ID)
		if err == nil && effectiveRole != "" {
			c.logger.Debug("access granted: per-project grant on list project", "username", username, "project", project.Slug, "effective_role", effectiveRole)
			return true
		}
		c.logger.Debug("access denied: not in access list", "username", username, "project", project.Slug, "user_id", user.ID)
		return false
	}
	// VisibilityCustom: per-project access (manual + LDAP + OAuth2 sources).
	effectiveRole, err := c.access.GetEffectiveRole(ctx, project.ID, user.ID)
	allowed := err == nil && effectiveRole != ""
	if allowed {
		c.logger.Debug("access granted: project-level access", "username", username, "project", project.Slug, "effective_role", effectiveRole)
	} else {
		c.logger.Debug("access denied: no project-level access", "username", username, "project", project.Slug, "user_id", user.ID)
	}
	return allowed
}

// CanUpload reports whether user is allowed to upload (or otherwise mutate
// versions of) project.
//
// Rules (current behavior — preserved):
//   - Anonymous: false.
//   - Global role admin or editor: true (regardless of visibility).
//   - VisibilityPrivate: true if a GlobalAccessGrant with editor or admin role exists.
//   - Otherwise: true iff ProjectAccess.GetEffectiveRole returns "editor" or "admin".
//
// Note the asymmetry with CanView for the global-editor case: a global editor
// can write to projects they can't read. The audit flagged this as M-2
// pending a separate product decision; preserve here for now.
func (c *Checker) CanUpload(ctx context.Context, user *database.User, project *database.Project) bool {
	if user == nil {
		return false
	}
	if user.Role == "admin" || user.Role == "editor" {
		c.logger.Debug("upload granted: global role", "username", user.Username, "project", project.Slug, "role", user.Role)
		return true
	}
	if project.Visibility == database.VisibilityPrivate {
		if role := c.globalRole(ctx, user); role == "editor" || role == "admin" {
			c.logger.Debug("upload granted: global access", "username", user.Username, "project", project.Slug, "global_role", role)
			return true
		}
	}
	if project.Visibility == database.VisibilityList {
		if role := c.listRole(ctx, user, project); role == "editor" {
			c.logger.Debug("upload granted: access list", "username", user.Username, "project", project.Slug, "list_role", role)
			return true
		}
	}
	effectiveRole, err := c.access.GetEffectiveRole(ctx, project.ID, user.ID)
	if err != nil {
		c.logger.Debug("upload denied: error checking project access", "username", user.Username, "project", project.Slug, "error", err)
		return false
	}
	allowed := effectiveRole == "editor" || effectiveRole == "admin"
	if allowed {
		c.logger.Debug("upload granted: project-level access", "username", user.Username, "project", project.Slug, "effective_role", effectiveRole)
	} else {
		c.logger.Debug("upload denied: insufficient project role", "username", user.Username, "project", project.Slug, "effective_role", effectiveRole)
	}
	return allowed
}

// CanManage reports whether user may administer project: edit its settings
// and grant/revoke per-project access. This is deliberately narrower than
// CanUpload — a global editor can upload to many projects, but only the
// project's creator (or a global admin) may manage it.
//
//   - Anonymous: false.
//   - Global role admin: true (admins manage everything).
//   - Otherwise: true iff the user is the recorded creator (project.CreatedBy).
//
// Note this needs no store lookups — it decides purely from the user role and
// the project's created_by column — so it takes no context.
func (c *Checker) CanManage(user *database.User, project *database.Project) bool {
	if user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	return project.CreatedBy != nil && *project.CreatedBy == user.ID
}

// FilterAccessible returns the subset of `all` that user can view.
// The membership rule is intentionally the same as CanView, just expressed
// in a way that minimizes DB round-trips for large project lists (one
// GlobalAccess lookup plus one ListAccessibleProjectIDs, rather than one
// CanView call per project).
func (c *Checker) FilterAccessible(ctx context.Context, user *database.User, all []database.Project) []database.Project {
	accessIDs, _ := c.access.ListAccessibleProjectIDs(ctx, user.ID)
	accessMap := make(map[int64]bool, len(accessIDs))
	for _, id := range accessIDs {
		accessMap[id] = true
	}

	hasGlobalAccess := c.globalRole(ctx, user) != ""

	// One lookup for every list the user is in, rather than one per project.
	listRoles := map[int64]string{}
	if c.accessLists != nil {
		if roles, err := c.accessLists.RolesForUser(ctx, user.ID, user.Username); err == nil {
			listRoles = roles
		}
	}

	var filtered []database.Project
	for _, p := range all {
		switch p.Visibility {
		case database.VisibilityPublic:
			filtered = append(filtered, p)
		case database.VisibilityPrivate:
			// Mirrors CanView: org-wide global grant OR per-project grant.
			if hasGlobalAccess || accessMap[p.ID] {
				filtered = append(filtered, p)
			}
		case database.VisibilityList:
			if p.AccessListID != nil && listRoles[*p.AccessListID] != "" || accessMap[p.ID] {
				filtered = append(filtered, p)
			}
		case database.VisibilityCustom:
			if accessMap[p.ID] {
				filtered = append(filtered, p)
			}
		}
	}
	return filtered
}

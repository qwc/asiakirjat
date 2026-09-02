package handler

import (
	"context"
	"errors"
	"fmt"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/projects"
)

// errNoCreateRights and errAmbiguousCreateOrg are the two ways a caller can be
// refused a new project: no organization admits them, or several do and none
// of them is the obvious one.
var (
	errNoCreateRights     = errors.New("no organization to create the project in")
	errAmbiguousCreateOrg = errors.New("several organizations to create the project in")
)

// canAutoCreate returns true if the user's instance role permits auto-creating
// projects anywhere. It is the first of two answers — a user without it may
// still create inside an organization it holds an editor grant on, which
// autoCreateOrg works out.
func canAutoCreate(user *database.User) bool {
	if user == nil {
		return false
	}
	return user.Role == "admin" || user.Role == "editor"
}

// autoCreateOrg decides which organization an auto-created project lands in,
// and in doing so decides whether the caller may create one at all (#155).
//
// Instance admins and editors keep creating in the default organization, as
// before. Everyone else — robots in particular, which are ordinary subjects
// now rather than blanket instance editors — creates in an organization they
// hold an editor grant on. Exactly one is unambiguous; several is not, and
// guessing would put the project in front of the wrong audience.
func (h *Handler) autoCreateOrg(ctx context.Context, user *database.User) (*int64, error) {
	if user == nil {
		return nil, errNoCreateRights
	}
	if canAutoCreate(user) {
		return nil, nil // the store lands it in the default org
	}

	orgs := h.resolver.OrgsWhereCanCreate(ctx, user)
	switch len(orgs) {
	case 0:
		return nil, errNoCreateRights
	case 1:
		return &orgs[0], nil
	default:
		return nil, fmt.Errorf("%w: %d", errAmbiguousCreateOrg, len(orgs))
	}
}

// autoCreateProject creates a new project reachable only through its grants,
// in orgID (nil for the default organization), and grants the creator admin
// over it. A concurrent create attempt with the same slug falls back to
// fetching the existing row.
func (h *Handler) autoCreateProject(ctx context.Context, slug string, creator *database.User, orgID *int64) (*database.Project, error) {
	project, err := h.projectService.Create(ctx, projects.CreateOptions{
		Slug:       slug,
		Visibility: database.VisibilityPrivate,
		OrgID:      orgID,
		Creator:    creator,
	})
	if errors.Is(err, projects.ErrSlugConflict) {
		// Race: another request created the slug between our check and ours.
		return h.projects.GetBySlug(ctx, slug)
	}
	if err != nil {
		return nil, err
	}
	h.logger.Info("auto-created project", "slug", slug, "creator", creator.Username)
	return project, nil
}

package handler

import (
	"context"
	"errors"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/projects"
)

// canAutoCreate returns true if the user has a role that permits auto-creating projects.
func canAutoCreate(user *database.User) bool {
	if user == nil {
		return false
	}
	return user.Role == "admin" || user.Role == "editor"
}

// autoCreateProject creates a new project (private visibility) and grants the
// creator editor access. A concurrent create attempt with the same slug
// falls back to fetching the existing row.
func (h *Handler) autoCreateProject(ctx context.Context, slug string, creator *database.User) (*database.Project, error) {
	project, err := h.projectService.Create(ctx, projects.CreateOptions{
		Slug:       slug,
		Visibility: database.VisibilityPrivate,
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

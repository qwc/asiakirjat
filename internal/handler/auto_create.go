package handler

import (
	"context"
	"regexp"
	"strings"

	"github.com/qwc/asiakirjat/internal/database"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// isValidSlug checks whether slug is a valid project slug (lowercase alphanumeric with hyphens, 1-128 chars).
func isValidSlug(slug string) bool {
	return len(slug) >= 1 && len(slug) <= 128 && slugPattern.MatchString(slug)
}

// canAutoCreate returns true if the user has a role that permits auto-creating projects.
func canAutoCreate(user *database.User) bool {
	if user == nil {
		return false
	}
	return user.Role == "admin" || user.Role == "editor"
}

// autoCreateProject creates a new project with the given slug, grants creator access, and returns it.
// If a race condition causes a duplicate create, it falls back to GetBySlug.
func (h *Handler) autoCreateProject(ctx context.Context, slug string, creator *database.User) (*database.Project, error) {
	project := &database.Project{
		Slug:       slug,
		Name:       slug,
		Visibility: database.VisibilityPrivate,
	}

	if err := h.projects.Create(ctx, project); err != nil {
		// Race condition: another request may have created it first
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate") {
			return h.projects.GetBySlug(ctx, slug)
		}
		return nil, err
	}

	if err := h.storage.EnsureProjectDir(slug); err != nil {
		h.logger.Error("creating project directory for auto-created project", "error", err)
	}

	// Auto-grant editor access to the creator for non-admin users
	if creator.Role != "admin" {
		access := &database.ProjectAccess{
			ProjectID: project.ID,
			UserID:    creator.ID,
			Role:      "editor",
		}
		if err := h.access.Grant(ctx, access); err != nil {
			h.logger.Error("auto-granting creator access", "error", err)
		}
	}

	h.logger.Info("auto-created project", "slug", slug, "creator", creator.Username)
	return project, nil
}

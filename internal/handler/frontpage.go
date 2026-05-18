package handler

import (
	"context"
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
)

type projectCardData struct {
	Name          string
	Slug          string
	Description   string
	Visibility    string
	LatestVersion string
}

// latestVersionTag returns the "latest" version tag.
// If pinnedVersion is set and exists in the list, it takes priority.
// Otherwise, falls back to the highest semver-sorted tag.
func latestVersionTag(versions []database.Version, pinnedVersion *string) string {
	if len(versions) == 0 {
		return ""
	}
	if pinnedVersion != nil {
		for _, v := range versions {
			if v.Tag == *pinnedVersion {
				return *pinnedVersion
			}
		}
	}
	tags := make([]string, len(versions))
	for i, v := range versions {
		tags[i] = v.Tag
	}
	docs.SortVersionTags(tags)
	return tags[0]
}

// filterAccessibleProjects is a thin forwarder to access.Checker.FilterAccessible;
// the rule lives in internal/access.
func (h *Handler) filterAccessibleProjects(ctx context.Context, user *database.User, all []database.Project) []database.Project {
	return h.checker.FilterAccessible(ctx, user, all)
}

func (h *Handler) handleFrontpage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	var dbProjects []database.Project

	if user != nil && user.Role == "admin" {
		all, err := h.projects.List(ctx)
		if err != nil {
			h.logger.Error("listing projects", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		dbProjects = all
	} else if user != nil {
		all, err := h.projects.List(ctx)
		if err != nil {
			h.logger.Error("listing projects", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		dbProjects = h.filterAccessibleProjects(ctx, user, all)
	} else {
		public, err := h.projects.ListByVisibility(ctx, database.VisibilityPublic)
		if err != nil {
			h.logger.Error("listing public projects", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		dbProjects = public
	}

	var projects []projectCardData
	for _, p := range dbProjects {
		card := projectCardData{
			Name:        p.Name,
			Slug:        p.Slug,
			Description: p.Description,
			Visibility:  p.Visibility,
		}
		versions, _ := h.versions.ListByProject(ctx, p.ID)
		card.LatestVersion = latestVersionTag(versions, p.PinnedVersion)
		projects = append(projects, card)
	}

	h.render(w, "frontpage", map[string]any{
		"User":     user,
		"Projects": projects,
	})
}

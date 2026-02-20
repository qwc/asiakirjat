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

// latestVersionTag returns the highest semver tag from a list of versions.
func latestVersionTag(versions []database.Version) string {
	if len(versions) == 0 {
		return ""
	}
	tags := make([]string, len(versions))
	for i, v := range versions {
		tags[i] = v.Tag
	}
	docs.SortVersionTags(tags)
	return tags[0]
}

// filterAccessibleProjects returns only the projects the user has access to.
func (h *Handler) filterAccessibleProjects(ctx context.Context, user *database.User, all []database.Project) []database.Project {
	accessIDs, _ := h.access.ListAccessibleProjectIDs(ctx, user.ID)
	accessMap := make(map[int64]bool)
	for _, id := range accessIDs {
		accessMap[id] = true
	}

	var hasGlobalAccess bool
	if h.globalAccess != nil {
		grant, err := h.globalAccess.GetGrantByUser(ctx, user.ID)
		if err == nil && grant != nil {
			hasGlobalAccess = true
		}
	}

	var filtered []database.Project
	for _, p := range all {
		switch p.Visibility {
		case database.VisibilityPublic:
			filtered = append(filtered, p)
		case database.VisibilityPrivate:
			if hasGlobalAccess {
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
		card.LatestVersion = latestVersionTag(versions)
		projects = append(projects, card)
	}

	h.render(w, "frontpage", map[string]any{
		"User":     user,
		"Projects": projects,
	})
}

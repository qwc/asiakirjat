package handler

import (
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

func (h *Handler) handleFrontpage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	var projects []projectCardData

	if user != nil && user.Role == "admin" {
		// Admin sees all projects
		all, err := h.projects.List(ctx)
		if err != nil {
			h.logger.Error("listing projects", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		for _, p := range all {
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
	} else if user != nil {
		// Authenticated user sees public + accessible projects
		public, err := h.projects.ListByVisibility(ctx, database.VisibilityPublic)
		if err != nil {
			h.logger.Error("listing public projects", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		accessIDs, _ := h.access.ListAccessibleProjectIDs(ctx, user.ID)
		accessMap := make(map[int64]bool)
		for _, id := range accessIDs {
			accessMap[id] = true
		}

		// Check global access for private projects
		var hasGlobalAccess bool
		if h.globalAccess != nil {
			grant, err := h.globalAccess.GetGrantByUser(ctx, user.ID)
			if err == nil && grant != nil {
				hasGlobalAccess = true
			}
		}

		all, _ := h.projects.List(ctx)
		seen := make(map[int64]bool)

		for _, p := range public {
			seen[p.ID] = true
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

		for _, p := range all {
			if seen[p.ID] {
				continue
			}
			// Private projects: user needs global access
			if p.Visibility == database.VisibilityPrivate && hasGlobalAccess {
				seen[p.ID] = true
				card := projectCardData{
					Name:        p.Name,
					Slug:        p.Slug,
					Description: p.Description,
					Visibility:  p.Visibility,
				}
				versions, _ := h.versions.ListByProject(ctx, p.ID)
				card.LatestVersion = latestVersionTag(versions)
				projects = append(projects, card)
				continue
			}
			// Custom projects: user needs explicit access
			if !accessMap[p.ID] {
				continue
			}
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
	} else {
		// Anonymous user sees only public projects
		public, err := h.projects.ListByVisibility(ctx, database.VisibilityPublic)
		if err != nil {
			h.logger.Error("listing public projects", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		for _, p := range public {
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
	}

	h.render(w, "frontpage", map[string]any{
		"User":     user,
		"Projects": projects,
	})
}

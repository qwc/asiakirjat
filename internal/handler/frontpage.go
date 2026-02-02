package handler

import (
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
)

type projectCardData struct {
	Name          string
	Slug          string
	Description   string
	IsPublic      bool
	LatestVersion string
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
				IsPublic:    p.IsPublic,
			}
			versions, _ := h.versions.ListByProject(ctx, p.ID)
			if len(versions) > 0 {
				card.LatestVersion = versions[0].Tag
			}
			projects = append(projects, card)
		}
	} else if user != nil {
		// Authenticated user sees public + accessible projects
		public, err := h.projects.ListPublic(ctx)
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

		all, _ := h.projects.List(ctx)
		seen := make(map[int64]bool)

		for _, p := range public {
			seen[p.ID] = true
			card := projectCardData{
				Name:        p.Name,
				Slug:        p.Slug,
				Description: p.Description,
				IsPublic:    p.IsPublic,
			}
			versions, _ := h.versions.ListByProject(ctx, p.ID)
			if len(versions) > 0 {
				card.LatestVersion = versions[0].Tag
			}
			projects = append(projects, card)
		}

		for _, p := range all {
			if seen[p.ID] {
				continue
			}
			if !accessMap[p.ID] {
				continue
			}
			card := projectCardData{
				Name:        p.Name,
				Slug:        p.Slug,
				Description: p.Description,
				IsPublic:    p.IsPublic,
			}
			versions, _ := h.versions.ListByProject(ctx, p.ID)
			if len(versions) > 0 {
				card.LatestVersion = versions[0].Tag
			}
			projects = append(projects, card)
		}
	} else {
		// Anonymous user sees only public projects
		public, err := h.projects.ListPublic(ctx)
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
				IsPublic:    p.IsPublic,
			}
			versions, _ := h.versions.ListByProject(ctx, p.ID)
			if len(versions) > 0 {
				card.LatestVersion = versions[0].Tag
			}
			projects = append(projects, card)
		}
	}

	h.render(w, "frontpage", map[string]any{
		"User":     user,
		"Projects": projects,
	})
}

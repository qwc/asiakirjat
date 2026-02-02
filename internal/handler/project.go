package handler

import (
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/docs"
)

type versionViewData struct {
	Tag       string
	URL       string
	CreatedAt interface{ Format(string) string }
}

func (h *Handler) handleProjectDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Access check
	if !project.IsPublic {
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if user.Role != "admin" {
			access, err := h.access.GetAccess(ctx, project.ID, user.ID)
			if err != nil || access == nil {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
	}

	versions, err := h.versions.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error("listing versions", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Sort versions by semver descending
	tags := make([]string, len(versions))
	versionMap := make(map[string]int)
	for i, v := range versions {
		tags[i] = v.Tag
		versionMap[v.Tag] = i
	}
	docs.SortVersionTags(tags)

	var versionViews []versionViewData
	for _, tag := range tags {
		v := versions[versionMap[tag]]
		versionViews = append(versionViews, versionViewData{
			Tag:       v.Tag,
			URL:       "/project/" + slug + "/" + v.Tag + "/",
			CreatedAt: v.CreatedAt,
		})
	}

	canUpload := false
	if user != nil {
		if user.Role == "admin" || user.Role == "editor" {
			canUpload = true
		} else {
			access, err := h.access.GetAccess(ctx, project.ID, user.ID)
			if err == nil && access != nil && (access.Role == "editor" || access.Role == "admin") {
				canUpload = true
			}
		}
	}

	h.render(w, "project_detail", map[string]any{
		"User":      user,
		"Project":   project,
		"Versions":  versionViews,
		"CanUpload": canUpload,
	})
}

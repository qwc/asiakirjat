package handler

import (
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/docs"
)

type versionViewData struct {
	Tag         string
	URL         string
	CreatedAt   interface{ Format(string) string }
	ProjectSlug string
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
			Tag:         v.Tag,
			URL:         "/project/" + slug + "/" + v.Tag + "/",
			CreatedAt:   v.CreatedAt,
			ProjectSlug: slug,
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

	// Build base URL for API examples
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	h.render(w, "project_detail", map[string]any{
		"User":      user,
		"Project":   project,
		"Versions":  versionViews,
		"CanUpload": canUpload,
		"CanDelete": canUpload,
		"BaseURL":   baseURL,
	})
}

func (h *Handler) handleDeleteVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")
	tag := r.PathValue("tag")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Check editor access (same logic as canUpload)
	canDelete := false
	if user.Role == "admin" || user.Role == "editor" {
		canDelete = true
	} else {
		access, err := h.access.GetAccess(ctx, project.ID, user.ID)
		if err == nil && access != nil && (access.Role == "editor" || access.Role == "admin") {
			canDelete = true
		}
	}

	if !canDelete {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	version, err := h.versions.GetByProjectAndTag(ctx, project.ID, tag)
	if err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	// Delete from database
	if err := h.versions.Delete(ctx, version.ID); err != nil {
		h.logger.Error("deleting version from database", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Delete from filesystem
	if err := h.storage.DeleteVersion(slug, tag); err != nil {
		h.logger.Error("deleting version from filesystem", "error", err)
		// Continue - database record is already deleted
	}

	// Delete from search index
	if h.searchIndex != nil {
		if err := h.searchIndex.DeleteVersion(project.ID, version.ID); err != nil {
			h.logger.Error("deleting version from search index", "error", err)
			// Continue - not critical
		}
	}

	// Invalidate latest tags cache
	h.invalidateLatestTagsCache()

	h.logger.Info("version deleted", "project", slug, "version", tag, "user", user.Username)
	http.Redirect(w, r, "/project/"+slug, http.StatusSeeOther)
}

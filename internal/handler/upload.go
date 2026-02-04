package handler

import (
	"context"
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
)

const maxUploadSize = 100 << 20 // 100 MB

func (h *Handler) handleUploadForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	h.render(w, "upload", map[string]any{
		"User":    user,
		"Project": project,
	})
}

func (h *Handler) handleUploadSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		h.render(w, "upload", map[string]any{
			"User":    user,
			"Project": project,
			"Error":   "File too large (max 100 MB)",
		})
		return
	}

	versionTag := r.FormValue("version")
	if versionTag == "" {
		h.render(w, "upload", map[string]any{
			"User":    user,
			"Project": project,
			"Error":   "Version tag is required",
		})
		return
	}

	file, header, err := r.FormFile("archive")
	if err != nil {
		h.render(w, "upload", map[string]any{
			"User":    user,
			"Project": project,
			"Error":   "Archive file is required",
		})
		return
	}
	defer file.Close()

	// Extract archive to storage
	if err := h.storage.EnsureVersionDir(slug, versionTag); err != nil {
		h.logger.Error("creating version directory", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	destPath := h.storage.VersionPath(slug, versionTag)
	if err := docs.ExtractArchive(file, header.Filename, destPath); err != nil {
		h.storage.DeleteVersion(slug, versionTag)
		h.render(w, "upload", map[string]any{
			"User":    user,
			"Project": project,
			"Error":   "Failed to extract archive: " + err.Error(),
		})
		return
	}

	// Check if version already exists (for re-upload)
	existingVersion, _ := h.versions.GetByProjectAndTag(ctx, project.ID, versionTag)
	isReupload := existingVersion != nil

	var version *database.Version
	if isReupload {
		// Update existing version
		existingVersion.StoragePath = destPath
		existingVersion.UploadedBy = user.ID
		if err := h.versions.Update(ctx, existingVersion); err != nil {
			h.storage.DeleteVersion(slug, versionTag)
			h.logger.Error("updating version record", "error", err)
			h.render(w, "upload", map[string]any{
				"User":    user,
				"Project": project,
				"Error":   "Failed to update version",
			})
			return
		}
		version = existingVersion

		// Delete old index entries before reindexing
		if h.searchIndex != nil {
			h.searchIndex.DeleteVersion(project.ID, version.ID)
		}
	} else {
		// Create new version record
		version = &database.Version{
			ProjectID:   project.ID,
			Tag:         versionTag,
			StoragePath: destPath,
			UploadedBy:  user.ID,
		}
		if err := h.versions.Create(ctx, version); err != nil {
			h.storage.DeleteVersion(slug, versionTag)
			h.logger.Error("creating version record", "error", err)
			h.render(w, "upload", map[string]any{
				"User":    user,
				"Project": project,
				"Error":   "Failed to create version",
			})
			return
		}
	}

	// Invalidate latest tags cache
	h.invalidateLatestTagsCache()

	// Async index for full-text search
	if h.searchIndex != nil {
		go func() {
			if err := h.searchIndex.IndexVersion(project.ID, version.ID, slug, project.Name, versionTag, destPath); err != nil {
				h.logger.Error("indexing version", "error", err, "project", slug, "version", versionTag)
			}
		}()
	}

	h.redirect(w, r, "/project/"+slug, http.StatusSeeOther)
}

func (h *Handler) canUpload(ctx context.Context, user *database.User, project *database.Project) bool {
	if user == nil {
		return false
	}
	// Check user's global role first
	if user.Role == "admin" || user.Role == "editor" {
		return true
	}
	// For private projects, check global access grants for editor role
	if project.Visibility == database.VisibilityPrivate && h.globalAccess != nil {
		grant, err := h.globalAccess.GetGrantByUser(ctx, user.ID)
		if err == nil && grant != nil && (grant.Role == "editor" || grant.Role == "admin") {
			return true
		}
	}
	// Check project-level access (from all sources: manual, ldap, oauth2)
	effectiveRole, err := h.access.GetEffectiveRole(ctx, project.ID, user.ID)
	if err != nil {
		return false
	}
	return effectiveRole == "editor" || effectiveRole == "admin"
}

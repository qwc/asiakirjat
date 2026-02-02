package handler

import (
	"net/http"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/docs"
)

func (h *Handler) handleServeDoc(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")
	version := r.PathValue("version")
	filePath := r.PathValue("path")

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

	ver, err := h.versions.GetByProjectAndTag(ctx, project.ID, version)
	if err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	storagePath := h.storage.VersionPath(slug, ver.Tag)

	// Inject overlay for HTML responses
	if filePath == "" || strings.HasSuffix(filePath, "/") || strings.HasSuffix(filePath, ".html") || strings.HasSuffix(filePath, ".htm") || !strings.Contains(filePath, ".") {
		// This might be an HTML file — let ServeDoc handle it, then we can inject overlay
		// For now, just serve the file directly; overlay injection comes in Phase 6
	}

	docs.ServeDoc(w, r, storagePath, filePath)
}

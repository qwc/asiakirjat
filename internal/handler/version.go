package handler

import (
	"html/template"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
	"github.com/qwc/asiakirjat/internal/templates"
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
	if !h.canViewProject(ctx, user, project) {
		if user == nil {
			h.redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	ver, err := h.versions.GetByProjectAndTag(ctx, project.ID, version)
	if err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	h.serveVersionDoc(w, r, slug, project.Name, ver, filePath)
}

// serveVersionDoc serves filePath from a specific version of a project,
// handling raw PDFs, the PDF viewer wrapper, and HTML overlay injection.
// Shared by handleServeDoc (explicit version in the URL) and handleServeLatest
// (rolling latest served in place), so both behave identically.
func (h *Handler) serveVersionDoc(w http.ResponseWriter, r *http.Request, slug, projectName string, ver *database.Version, filePath string) {
	storagePath := h.storage.VersionPath(slug, ver.Tag)

	// PDF version handling
	if ver.ContentType == "pdf" {
		if filePath == "document.pdf" {
			// Serve the raw PDF file
			http.ServeFile(w, r, filepath.Join(storagePath, "document.pdf"))
			return
		}
		// Render PDF viewer wrapper page
		h.servePDFViewer(w, r, slug, projectName, ver.Tag, storagePath)
		return
	}

	// For paths that might be HTML, inject the overlay toolbar
	maybeHTML := filePath == "" ||
		strings.HasSuffix(filePath, "/") ||
		strings.HasSuffix(filePath, ".html") ||
		strings.HasSuffix(filePath, ".htm") ||
		!strings.Contains(filePath, ".")

	if maybeHTML {
		overlayHTML, err := h.templates.RenderOverlay(templates.OverlayData{
			Slug:        slug,
			ProjectName: projectName,
			Version:     ver.Tag,
		})
		if err != nil {
			h.logger.Error("rendering overlay", "error", err)
			docs.ServeDoc(w, r, storagePath, filePath)
			return
		}

		docs.InjectOverlay(w, r, overlayHTML, func(rw http.ResponseWriter, req *http.Request) {
			docs.ServeDoc(rw, req, storagePath, filePath)
		})
		return
	}

	docs.ServeDoc(w, r, storagePath, filePath)
}

// handleServeLatest serves a project's current newest version in place under
// the stable URL /project/{slug}/latest/{path...}. Because the content is
// served at the /latest/ URL (rather than redirecting to a versioned URL), the
// address stays stable and relative links inside the docs keep resolving under
// /latest/ — so a visitor who lands on the permalink keeps browsing "latest".
//
// "Latest" is resolved with the same rule used for the project page's "Latest"
// badge and the frontpage (latestVersionTag): a pinned version if set,
// otherwise the highest semver tag. Responses are marked no-cache so a newer
// upload is picked up on the next request rather than served stale.
func (h *Handler) handleServeLatest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")
	filePath := r.PathValue("path")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Same access gate as handleServeDoc — must not leak the existence of
	// versions on a project the user can't see.
	if !h.canViewProject(ctx, user, project) {
		if user == nil {
			h.redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	versions, err := h.versions.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error("listing versions", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	tag := latestVersionTag(versions, project.PinnedVersion)
	if tag == "" {
		http.Error(w, "No versions available", http.StatusNotFound)
		return
	}
	ver, err := h.versions.GetByProjectAndTag(ctx, project.ID, tag)
	if err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	// "latest" is a rolling pointer: force revalidation so a new upload is
	// reflected on the next request instead of being served from cache.
	w.Header().Set("Cache-Control", "no-cache")
	h.serveVersionDoc(w, r, slug, project.Name, ver, filePath)
}

// handleLatestSlashRedirect normalizes /project/{slug}/latest (no trailing
// slash) to /project/{slug}/latest/ so the served index page's relative links
// resolve under /latest/. The slash redirect itself is permanent (it never
// depends on which version is latest).
func (h *Handler) handleLatestSlashRedirect(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Path + "/"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

func (h *Handler) servePDFViewer(w http.ResponseWriter, r *http.Request, slug, projectName, version, storagePath string) {
	overlayHTML, err := h.templates.RenderOverlay(templates.OverlayData{
		Slug:        slug,
		ProjectName: projectName,
		Version:     version,
	})
	if err != nil {
		h.logger.Error("rendering overlay for PDF viewer", "error", err)
		// Fall back to serving the raw PDF
		http.ServeFile(w, r, filepath.Join(storagePath, "document.pdf"))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.RenderPDFViewer(w, templates.PDFViewerData{
		ProjectName: projectName,
		Version:     version,
		OverlayHTML: template.HTML(overlayHTML),
	}); err != nil {
		h.logger.Error("rendering PDF viewer", "error", err)
	}
}

package handler

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
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

	storagePath := h.storage.VersionPath(slug, ver.Tag)

	// PDF version handling
	if ver.ContentType == "pdf" {
		if filePath == "document.pdf" {
			// Serve the raw PDF file
			http.ServeFile(w, r, filepath.Join(storagePath, "document.pdf"))
			return
		}
		// Render PDF viewer wrapper page
		h.servePDFViewer(w, r, slug, project.Name, ver.Tag, storagePath)
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
			ProjectName: project.Name,
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
	fmt.Fprintf(w, `<!DOCTYPE html><html>
<head><meta charset="utf-8"><title>%s - %s</title>
<style>html,body{margin:0;height:100%%;overflow:hidden}</style>
</head><body>
%s
<embed id="pdf-embed" src="document.pdf" type="application/pdf"
       style="position:fixed;left:0;right:0;width:100%%;border:none">
<script>
(function(){
var o=document.getElementById('asiakirjat-overlay');
var e=document.getElementById('pdf-embed');
function fit(){var h=o?o.offsetHeight:0;e.style.top=h+'px';e.style.height='calc(100vh - '+h+'px)';}
fit();window.addEventListener('resize',fit);
})();
</script>
</body></html>`, projectName, version, overlayHTML)
}

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
	"github.com/qwc/asiakirjat/internal/projects"
	"github.com/qwc/asiakirjat/internal/validation"
)

func (h *Handler) handleAPIProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	query := r.URL.Query().Get("q")

	var projects []database.Project
	var err error

	if query != "" {
		projects, err = h.projects.Search(ctx, query)
	} else {
		projects, err = h.projects.List(ctx)
	}

	if err != nil {
		h.jsonError(w, "Failed to list projects", http.StatusInternalServerError)
		return
	}

	// Filter based on access
	var filtered []database.Project
	for _, p := range projects {
		if h.canViewProject(ctx, user, &p) {
			filtered = append(filtered, p)
		}
	}

	type projectJSON struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	}

	result := make([]projectJSON, 0, len(filtered))
	for _, p := range filtered {
		result = append(result, projectJSON{
			Slug:        p.Slug,
			Name:        p.Name,
			Description: p.Description,
			Visibility:  p.Visibility,
		})
	}

	h.jsonResponse(w, result)
}

func (h *Handler) handleAPIVersions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		h.jsonError(w, "Project not found", http.StatusNotFound)
		return
	}

	// Hide project existence from callers who can't view it.
	if !h.canViewProject(ctx, user, project) {
		h.jsonError(w, "Project not found", http.StatusNotFound)
		return
	}

	versions, err := h.versions.ListByProject(ctx, project.ID)
	if err != nil {
		h.jsonError(w, "Failed to list versions", http.StatusInternalServerError)
		return
	}

	// Sort versions by semver (descending)
	tags := make([]string, len(versions))
	versionMap := make(map[string]database.Version)
	for i, v := range versions {
		tags[i] = v.Tag
		versionMap[v.Tag] = v
	}
	docs.SortVersionTags(tags)

	type versionJSON struct {
		Tag         string `json:"tag"`
		ContentType string `json:"content_type"`
		CreatedAt   string `json:"created_at"`
	}

	result := make([]versionJSON, 0, len(tags))
	for _, tag := range tags {
		v := versionMap[tag]
		result = append(result, versionJSON{
			Tag:         v.Tag,
			ContentType: v.ContentType,
			CreatedAt:   v.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	h.jsonResponse(w, result)
}

func (h *Handler) handleAPIUpload(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	h.handleAPIUploadWithSlug(w, r, slug)
}

func (h *Handler) handleAPIUploadGeneral(w http.ResponseWriter, r *http.Request) {
	// Parse form first to get the project slug
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		h.jsonError(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	slug := r.FormValue("project")
	if slug == "" {
		h.jsonError(w, "Project slug is required", http.StatusBadRequest)
		return
	}

	h.handleAPIUploadWithSlug(w, r, slug)
}

func (h *Handler) handleAPIUploadWithSlug(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()

	project, err := h.projects.GetBySlug(ctx, slug)
	var user *database.User
	if err != nil {
		// Project doesn't exist — try auto-create path
		if h.config.Projects.AutoCreate && validation.IsValidSlug(slug) {
			// No project to scope to, so use unscoped auth
			user = h.tokenAuth.AuthenticateRequest(r)
			if user == nil {
				h.jsonError(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if !canAutoCreate(user) {
				h.jsonError(w, "Forbidden: insufficient role to auto-create projects", http.StatusForbidden)
				return
			}
			project, err = h.autoCreateProject(ctx, slug, user)
			if err != nil {
				h.logger.Error("auto-creating project", "error", err)
				h.jsonError(w, "Failed to create project", http.StatusInternalServerError)
				return
			}
		} else {
			h.jsonError(w, "Project not found", http.StatusNotFound)
			return
		}
	} else {
		// Project exists — use project-scoped auth
		user = h.tokenAuth.AuthenticateRequestForProject(r, project.ID)
		if user == nil {
			h.jsonError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	if !h.canUpload(ctx, user, project) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Parse form if not already parsed (for path-based endpoint)
	if r.MultipartForm == nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
		if err := r.ParseMultipartForm(maxUploadSize); err != nil {
			h.jsonError(w, "File too large", http.StatusRequestEntityTooLarge)
			return
		}
	}

	versionTag := r.FormValue("version")
	if versionTag == "" {
		h.jsonError(w, "Version tag is required", http.StatusBadRequest)
		return
	}
	if !validation.IsValidVersionTag(versionTag) {
		h.jsonError(w, "Invalid version tag: must be 1-64 chars, starting with a letter or digit, then letters/digits/dots/underscores/hyphens only", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("archive")
	if err != nil {
		h.jsonError(w, "File is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	isPDF := strings.HasSuffix(strings.ToLower(header.Filename), ".pdf")

	if err := h.storage.EnsureVersionDir(slug, versionTag); err != nil {
		h.logger.Error("creating version directory", "error", err)
		h.jsonError(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	destPath := h.storage.VersionPath(slug, versionTag)
	contentType := "archive"

	if isPDF {
		contentType = "pdf"
		if err := storePDF(file, destPath); err != nil {
			h.storage.DeleteVersion(slug, versionTag)
			h.jsonError(w, "Failed to store PDF: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		if err := docs.ExtractArchive(file, header.Filename, destPath); err != nil {
			h.storage.DeleteVersion(slug, versionTag)
			h.jsonError(w, "Failed to extract archive: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Check if version already exists (for re-upload)
	existingVersion, _ := h.versions.GetByProjectAndTag(ctx, project.ID, versionTag)
	isReupload := existingVersion != nil

	uid := user.ID

	var version *database.Version
	if isReupload {
		// Update existing version
		existingVersion.StoragePath = destPath
		existingVersion.ContentType = contentType
		existingVersion.UploadedBy = &uid
		if err := h.versions.Update(ctx, existingVersion); err != nil {
			h.storage.DeleteVersion(slug, versionTag)
			h.jsonError(w, "Failed to update version", http.StatusInternalServerError)
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
			ContentType: contentType,
			UploadedBy:  &uid,
		}
		if err := h.versions.Create(ctx, version); err != nil {
			h.storage.DeleteVersion(slug, versionTag)
			h.jsonError(w, "Failed to create version", http.StatusConflict)
			return
		}
	}

	// Log the upload
	if h.uploadLogs != nil {
		uploadLog := &database.UploadLog{
			ProjectID:   project.ID,
			VersionTag:  versionTag,
			ContentType: contentType,
			UploadedBy:  &uid,
			IsReupload:  isReupload,
			Filename:    header.Filename,
		}
		if err := h.uploadLogs.Create(ctx, uploadLog); err != nil {
			h.logger.Error("creating upload log", "error", err)
		}
	}

	// Clear temporary pin on new version upload
	if !isReupload && project.PinnedVersion != nil && !project.PinPermanent {
		project.PinnedVersion = nil
		if err := h.projects.Update(ctx, project); err != nil {
			h.logger.Error("clearing temporary pin", "error", err)
		}
	}

	// Invalidate latest tags cache
	h.invalidateLatestTagsCache()

	// Async index for full-text search — tracked so shutdown can drain it.
	if h.searchIndex != nil {
		h.runJob(func(ctx context.Context) {
			if err := h.searchIndex.IndexVersion(project.ID, version.ID, slug, project.Name, versionTag, destPath); err != nil {
				h.logger.Error("indexing version", "error", err, "project", slug, "version", versionTag)
			}
		})
	}

	// Enforce retention after uploading a version the project does not keep
	// indefinitely — also tracked.
	if !isReupload && !h.versionKeeper(project)(versionTag) {
		h.runJob(func(ctx context.Context) {
			h.enforceRetentionPolicy(ctx, project)
		})
	}

	h.jsonResponse(w, map[string]string{
		"status":  "ok",
		"version": versionTag,
		"project": slug,
	})
}

func (h *Handler) handleAPICreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, token := h.tokenAuth.AuthenticateRequestWithToken(r)
	if user == nil {
		h.jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if user.Role != "admin" && user.Role != "editor" {
		h.jsonError(w, "Forbidden: admin or editor role required", http.StatusForbidden)
		return
	}

	// Project creation operates above a single project, so project-scoped
	// tokens are not permitted here — an editor token scoped to project A
	// must not be able to spawn project B. Use a global (unscoped) token.
	if token.ProjectID != nil {
		h.jsonError(w, "Forbidden: project-scoped tokens cannot create projects; use a global token", http.StatusForbidden)
		return
	}

	var req struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	project, err := h.projectService.Create(ctx, projects.CreateOptions{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
		Visibility:  req.Visibility,
		Creator:     user,
	})
	switch {
	case errors.Is(err, projects.ErrInvalidSlug):
		h.jsonError(w, "Invalid slug: must be 1-128 lowercase alphanumeric characters with hyphens", http.StatusBadRequest)
		return
	case errors.Is(err, projects.ErrInvalidVisibility):
		h.jsonError(w, "Invalid visibility: must be public, private, or custom", http.StatusBadRequest)
		return
	case errors.Is(err, projects.ErrPublicRequiresAdmin):
		h.jsonError(w, "Forbidden: only admins can create public projects", http.StatusForbidden)
		return
	case errors.Is(err, projects.ErrSlugConflict):
		h.jsonError(w, "Project with this slug already exists", http.StatusConflict)
		return
	case err != nil:
		h.logger.Error("creating project via API", "error", err)
		h.jsonError(w, "Failed to create project", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"slug":        project.Slug,
		"name":        project.Name,
		"description": project.Description,
		"visibility":  project.Visibility,
	})
}

func (h *Handler) handleHealthz(w http.ResponseWriter, r *http.Request) {
	h.jsonResponse(w, map[string]string{"status": "ok"})
}

func (h *Handler) jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) jsonError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

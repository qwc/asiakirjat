package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
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
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
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
	tokenAuth := auth.NewTokenAuthenticator(h.tokens, h.users)

	project, err := h.projects.GetBySlug(ctx, slug)
	var user *database.User
	if err != nil {
		// Project doesn't exist — try auto-create path
		if h.config.Projects.AutoCreate && isValidSlug(slug) {
			// No project to scope to, so use unscoped auth
			user = tokenAuth.AuthenticateRequest(r)
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
		user = tokenAuth.AuthenticateRequestForProject(r, project.ID)
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

	var version *database.Version
	if isReupload {
		// Update existing version
		existingVersion.StoragePath = destPath
		existingVersion.ContentType = contentType
		existingVersion.UploadedBy = user.ID
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
			UploadedBy:  user.ID,
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
			UploadedBy:  user.ID,
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

	// Async index for full-text search
	if h.searchIndex != nil {
		go func() {
			if err := h.searchIndex.IndexVersion(project.ID, version.ID, slug, project.Name, versionTag, destPath); err != nil {
				h.logger.Error("indexing version", "error", err, "project", slug, "version", versionTag)
			}
		}()
	}

	// Enforce retention after new non-semver upload
	if !isReupload && !docs.IsSemver(versionTag) {
		go h.enforceRetentionPolicy(context.Background(), project)
	}

	h.jsonResponse(w, map[string]string{
		"status":  "ok",
		"version": versionTag,
		"project": slug,
	})
}

func (h *Handler) handleAPICreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tokenAuth := auth.NewTokenAuthenticator(h.tokens, h.users)
	user := tokenAuth.AuthenticateRequest(r)
	if user == nil {
		h.jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if user.Role != "admin" && user.Role != "editor" {
		h.jsonError(w, "Forbidden: admin or editor role required", http.StatusForbidden)
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

	if !isValidSlug(req.Slug) {
		h.jsonError(w, "Invalid slug: must be 1-128 lowercase alphanumeric characters with hyphens", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		req.Name = req.Slug
	}
	if req.Visibility == "" {
		req.Visibility = database.VisibilityPrivate
	}
	if req.Visibility != database.VisibilityPublic && req.Visibility != database.VisibilityPrivate && req.Visibility != database.VisibilityCustom {
		h.jsonError(w, "Invalid visibility: must be public, private, or custom", http.StatusBadRequest)
		return
	}

	// Check for duplicate
	if existing, _ := h.projects.GetBySlug(ctx, req.Slug); existing != nil {
		h.jsonError(w, "Project with this slug already exists", http.StatusConflict)
		return
	}

	project := &database.Project{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
		Visibility:  req.Visibility,
	}

	if err := h.projects.Create(ctx, project); err != nil {
		h.logger.Error("creating project via API", "error", err)
		h.jsonError(w, "Failed to create project", http.StatusInternalServerError)
		return
	}

	if err := h.storage.EnsureProjectDir(req.Slug); err != nil {
		h.logger.Error("creating project directory", "error", err)
	}

	// Auto-grant editor access to the creator for non-admin, non-public projects
	if user.Role != "admin" && req.Visibility != database.VisibilityPublic {
		access := &database.ProjectAccess{
			ProjectID: project.ID,
			UserID:    user.ID,
			Role:      "editor",
		}
		if err := h.access.Grant(ctx, access); err != nil {
			h.logger.Error("auto-granting creator access", "error", err)
		}
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

package handler

import (
	"encoding/json"
	"net/http"

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
		Tag       string `json:"tag"`
		CreatedAt string `json:"created_at"`
	}

	result := make([]versionJSON, 0, len(tags))
	for _, tag := range tags {
		v := versionMap[tag]
		result = append(result, versionJSON{
			Tag:       v.Tag,
			CreatedAt: v.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	h.jsonResponse(w, result)
}

func (h *Handler) handleAPIUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")

	// Get project first to know the project ID for token scope validation
	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		h.jsonError(w, "Project not found", http.StatusNotFound)
		return
	}

	// Authenticate via Bearer token with project scope validation
	tokenAuth := auth.NewTokenAuthenticator(h.tokens, h.users)
	user := tokenAuth.AuthenticateRequestForProject(r, project.ID)
	if user == nil {
		h.jsonError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if !h.canUpload(ctx, user, project) {
		h.jsonError(w, "Forbidden", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		h.jsonError(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	versionTag := r.FormValue("version")
	if versionTag == "" {
		h.jsonError(w, "Version tag is required", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("archive")
	if err != nil {
		h.jsonError(w, "Archive file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if err := h.storage.EnsureVersionDir(slug, versionTag); err != nil {
		h.logger.Error("creating version directory", "error", err)
		h.jsonError(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	destPath := h.storage.VersionPath(slug, versionTag)
	if err := docs.ExtractArchive(file, header.Filename, destPath); err != nil {
		h.storage.DeleteVersion(slug, versionTag)
		h.jsonError(w, "Failed to extract archive: "+err.Error(), http.StatusBadRequest)
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
			UploadedBy:  user.ID,
		}
		if err := h.versions.Create(ctx, version); err != nil {
			h.storage.DeleteVersion(slug, versionTag)
			h.jsonError(w, "Failed to create version", http.StatusConflict)
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

	h.jsonResponse(w, map[string]string{
		"status":  "ok",
		"version": versionTag,
		"project": slug,
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

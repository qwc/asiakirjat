package handler

import (
	"net/http"
	"strconv"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
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

	// Sort versions by semver descending
	tags := make([]string, len(versions))
	versionMap := make(map[string]int)
	for i, v := range versions {
		tags[i] = v.Tag
		versionMap[v.Tag] = i
	}
	docs.SortVersionTags(tags)

	var versionViews []versionViewData
	bp := h.config.Server.BasePath
	for _, tag := range tags {
		v := versions[versionMap[tag]]
		versionViews = append(versionViews, versionViewData{
			Tag:         v.Tag,
			URL:         bp + "/project/" + slug + "/" + v.Tag + "/",
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
	h.redirect(w, r, "/project/"+slug, http.StatusSeeOther)
}

// handleProjectTokens lists API tokens scoped to this project.
func (h *Handler) handleProjectTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Check editor access
	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	tokens, err := h.tokens.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error("listing project tokens", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Build user lookup for token display
	users, _ := h.users.List(ctx)
	userNames := make(map[int64]string)
	for _, u := range users {
		userNames[u.ID] = u.Username
	}

	type tokenView struct {
		database.APIToken
		Username string
	}

	var tokenViews []tokenView
	for _, t := range tokens {
		tokenViews = append(tokenViews, tokenView{
			APIToken: t,
			Username: userNames[t.UserID],
		})
	}

	h.render(w, "project_tokens", map[string]any{
		"User":    user,
		"Project": project,
		"Tokens":  tokenViews,
	})
}

// handleProjectCreateToken creates a new API token scoped to this project.
// Editors can only create project-scoped tokens, not global tokens.
func (h *Handler) handleProjectCreateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Check editor access
	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		name = "default"
	}

	// Generate raw token
	rawToken, err := auth.GenerateToken(32)
	if err != nil {
		h.logger.Error("generating token", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	tokenHash := auth.HashToken(rawToken)

	// Editors can only create project-scoped tokens (projectID is always set)
	projectID := project.ID
	token := &database.APIToken{
		UserID:    user.ID,
		ProjectID: &projectID,
		TokenHash: tokenHash,
		Name:      name,
		Scopes:    "upload",
	}

	if err := h.tokens.Create(ctx, token); err != nil {
		h.logger.Error("creating token", "error", err)
		http.Error(w, "Failed to create token", http.StatusInternalServerError)
		return
	}

	// Re-render tokens page with the new token shown
	tokens, _ := h.tokens.ListByProject(ctx, project.ID)

	users, _ := h.users.List(ctx)
	userNames := make(map[int64]string)
	for _, u := range users {
		userNames[u.ID] = u.Username
	}

	type tokenView struct {
		database.APIToken
		Username string
	}

	var tokenViews []tokenView
	for _, t := range tokens {
		tokenViews = append(tokenViews, tokenView{
			APIToken: t,
			Username: userNames[t.UserID],
		})
	}

	h.render(w, "project_tokens", map[string]any{
		"User":     user,
		"Project":  project,
		"Tokens":   tokenViews,
		"NewToken": rawToken,
	})
}

// handleProjectRevokeToken revokes a token scoped to this project.
func (h *Handler) handleProjectRevokeToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Check editor access
	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	tokenID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	// Validate token belongs to this project
	token, err := h.tokens.GetByID(ctx, tokenID)
	if err != nil {
		http.Error(w, "Token not found", http.StatusNotFound)
		return
	}
	if token.ProjectID == nil || *token.ProjectID != project.ID {
		http.Error(w, "Token does not belong to this project", http.StatusForbidden)
		return
	}

	if err := h.tokens.Delete(ctx, tokenID); err != nil {
		h.logger.Error("revoking token", "error", err)
		http.Error(w, "Failed to revoke token", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/project/"+slug+"/tokens", http.StatusSeeOther)
}

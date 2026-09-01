package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/projects"
	"github.com/qwc/asiakirjat/internal/validation"
)

// Admin handlers for projects and their per-project access.

func (h *Handler) handleAdminProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	isAdmin := user != nil && user.Role == "admin"

	allProjects, err := h.projects.List(ctx)
	if err != nil {
		h.logger.Error("listing projects", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Admins see all projects; editors only see projects they have access to
	var projects []database.Project
	if isAdmin {
		projects = allProjects
	} else {
		projects = h.filterAccessibleProjects(ctx, user, allProjects)
	}

	// Resolve creator IDs to usernames for the "Created by" column. Build the
	// lookup from the full user list once.
	userMap := make(map[int64]string)
	if allUsers, err := h.users.List(ctx); err == nil {
		for _, u := range allUsers {
			userMap[u.ID] = u.Username
		}
	}

	// projectView decorates a project with its creator name and whether the
	// current user may manage it (so the template can show Edit/Delete only on
	// the rows that allow it — admins on all, editors on their own creations).
	type projectView struct {
		database.Project
		CreatedByName string
		CanManage     bool
	}
	projectViews := make([]projectView, 0, len(projects))
	for i := range projects {
		p := projects[i]
		name := ""
		if p.CreatedBy != nil {
			name = userMap[*p.CreatedBy]
		}
		projectViews = append(projectViews, projectView{
			Project:       p,
			CreatedByName: name,
			CanManage:     h.canManage(ctx, user, &p),
		})
	}

	reindexRunning, reindexProgress := h.reindex.snapshot()
	data := map[string]any{
		"User":            user,
		"IsAdmin":         isAdmin,
		"Projects":        projectViews,
		"ReindexRunning":  reindexRunning,
		"ReindexProgress": reindexProgress,
	}

	// Check for flash message from query parameter
	switch r.URL.Query().Get("msg") {
	case "reindex_started":
		data["Flash"] = &Flash{
			Type:    "success",
			Message: "Search index rebuild started in background",
		}
	case "reindex_already_running":
		data["Flash"] = &Flash{
			Type:    "warning",
			Message: "Reindex is already running",
		}
	case "docs_deployed":
		data["Flash"] = &Flash{
			Type:    "success",
			Message: "Built-in documentation deployed successfully",
		}
	case "visibility_restricted":
		data["Flash"] = &Flash{
			Type:    "warning",
			Message: "Project visibility changed from public — review project access so the intended users still have access.",
		}
	}

	h.render(w, r, "admin_projects", data)
}

func (h *Handler) handleAdminCreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse retention_days form input.
	var retentionDays *int
	if rd := r.FormValue("retention_days"); rd != "" {
		if days, err := strconv.Atoi(rd); err == nil && days >= 0 {
			retentionDays = &days
		}
	}

	visibility := r.FormValue("visibility")
	_, err := h.projectService.Create(ctx, projects.CreateOptions{
		Slug:          r.FormValue("slug"),
		Name:          r.FormValue("name"),
		Description:   r.FormValue("description"),
		Visibility:    visibility,
		RetentionDays: retentionDays,
		Creator:       auth.UserFromContext(ctx),
	})
	switch {
	case errors.Is(err, projects.ErrInvalidSlug):
		http.Error(w, "Invalid slug: must be 1-128 lowercase alphanumeric characters with single hyphens between segments", http.StatusBadRequest)
		return
	case errors.Is(err, projects.ErrInvalidVisibility):
		http.Error(w, "Invalid visibility: must be public, private, custom, or list", http.StatusBadRequest)
		return
	case errors.Is(err, projects.ErrListRequired):
		http.Error(w, "Choose an access list for list visibility", http.StatusBadRequest)
		return
	case errors.Is(err, projects.ErrPublicRequiresAdmin):
		http.Error(w, "Forbidden: only admins can create public projects", http.StatusForbidden)
		return
	case errors.Is(err, projects.ErrSlugConflict):
		http.Error(w, "Project with this slug already exists", http.StatusConflict)
		return
	case err != nil:
		h.logger.Error("creating project", "error", err)
		http.Error(w, "Failed to create project", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/admin/projects", http.StatusSeeOther)
}

func (h *Handler) handleAdminEditProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if !h.canManage(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	accessList, _ := h.access.ListByProject(ctx, project.ID)
	users, _ := h.users.List(ctx)

	type accessView struct {
		UserID   int64
		Username string
		Role     string
		Source   string
	}
	var accessViews []accessView
	hasSyncedAccess := false
	userMap := make(map[int64]string)
	for _, u := range users {
		userMap[u.ID] = u.Username
	}
	for _, a := range accessList {
		if a.Source != database.AccessSourceManual {
			hasSyncedAccess = true
		}
		accessViews = append(accessViews, accessView{
			UserID:   a.UserID,
			Username: userMap[a.UserID],
			Role:     a.Role,
			Source:   a.Source,
		})
	}

	createdByName := ""
	if project.CreatedBy != nil {
		createdByName = userMap[*project.CreatedBy]
	}

	// Build retention display info
	globalDefault := h.config.Retention.NonSemverDays
	retentionDisplay := ""
	if project.RetentionDays != nil {
		retentionDisplay = strconv.Itoa(*project.RetentionDays)
	}
	globalRetentionLabel := "unlimited"
	if globalDefault > 0 {
		globalRetentionLabel = strconv.Itoa(globalDefault) + " days"
	}

	grants, err := h.accessGrants.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error("listing project grants", "project", project.Slug, "error", err)
	}

	orgs, err := h.orgs.List(ctx)
	if err != nil {
		h.logger.Error("listing orgs", "error", err)
	}

	h.render(w, r, "admin_project_edit", map[string]any{
		"User":                   user,
		"Grants":                 h.grantViews(ctx, grants),
		"AccessGroups":           h.availableAccessGroups(ctx),
		"Orgs":                   orgs,
		"CurrentOrgID":           currentOrgID(project),
		"IsAdmin":                user != nil && user.Role == "admin",
		"Project":                project,
		"CreatedByName":          createdByName,
		"AccessList":             accessViews,
		"HasSyncedAccess":        hasSyncedAccess,
		"Users":                  users,
		"RetentionDisplay":       retentionDisplay,
		"VersionKeepPattern":     keepPatternDisplay(project),
		"GlobalRetentionDefault": globalRetentionLabel,
	})
}

func (h *Handler) handleAdminUpdateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if !h.canManage(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	newSlug := r.FormValue("slug")
	if !validation.IsValidSlug(newSlug) {
		http.Error(w, "Invalid slug: must be 1-128 lowercase alphanumeric characters with single hyphens between segments", http.StatusBadRequest)
		return
	}
	previousVisibility := project.Visibility
	project.Slug = newSlug
	project.Name = r.FormValue("name")
	project.Description = r.FormValue("description")
	// One rule for both paths: projects.ValidateVisibility is what
	// Service.Create applies, including that only admins may publish and
	// that list visibility needs the list it points at.
	// The form posts exposure; visibility is derived when absent, so a caller
	// that only knows the new field is not rejected. Both are kept in step
	// until the visibility column goes.
	exposure := r.FormValue("exposure")
	if exposure == "" {
		exposure = project.Exposure
	}
	visibility := r.FormValue("visibility")
	if visibility == "" {
		visibility = visibilityForExposure(exposure, project.Visibility)
	}
	// The access-list picker is gone: a project's audience is expressed with
	// grants now. ValidateVisibility still guards who may publish publicly.
	switch err := projects.ValidateVisibility(visibility, nil, user); {
	case errors.Is(err, projects.ErrPublicRequiresAdmin):
		http.Error(w, "Forbidden: only admins can make projects public", http.StatusForbidden)
		return
	case errors.Is(err, projects.ErrListRequired):
		http.Error(w, "Choose an access list for list visibility", http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, "Invalid visibility", http.StatusBadRequest)
		return
	}
	project.Visibility = visibility

	// Exposure is what the checker reads now (#150, #151); visibility is kept
	// in step with it until the column goes, so a downgrade of this build does
	// not resurrect a stale value.
	if !database.ValidExposure(exposure) {
		http.Error(w, "Invalid exposure", http.StatusBadRequest)
		return
	}
	// Only admins may publish to signed-out visitors, matching the rule that
	// already governed public visibility.
	if exposure == database.ExposurePublic && (user == nil || user.Role != "admin") {
		http.Error(w, "Only admins can make a project public", http.StatusForbidden)
		return
	}
	project.Exposure = exposure

	if orgID, err := strconv.ParseInt(r.FormValue("org_id"), 10, 64); err == nil && orgID > 0 {
		project.OrgID = &orgID
	}
	// Any list a project still points at is inert: the checker reads grants.
	// Clearing it keeps the row from implying otherwise.
	project.AccessListID = nil

	// A version keep pattern is optional; empty clears it and restores the
	// "keep semver tags" default.
	keepPattern := strings.TrimSpace(r.FormValue("version_keep_pattern"))
	if err := projects.ValidateVersionKeepPattern(keepPattern); err != nil {
		// Deliberately verbose: this error describes the regular expression
		// the admin just typed ("missing closing )"), which is the whole
		// point of showing it. Nothing internal is quoted.
		http.Error(w, "Invalid version keep pattern: "+err.Error(), http.StatusBadRequest)
		return
	}
	if keepPattern == "" {
		project.VersionKeepPattern = nil
	} else {
		project.VersionKeepPattern = &keepPattern
	}

	// Parse retention_days: empty = NULL (use global default), "0" = unlimited, positive = override
	if rd := r.FormValue("retention_days"); rd == "" {
		project.RetentionDays = nil
	} else if days, err := strconv.Atoi(rd); err == nil && days >= 0 {
		project.RetentionDays = &days
	} else {
		project.RetentionDays = nil
	}

	// Persist under the per-project lock so a rename (which moves the on-disk
	// directory) can't race an in-flight archive extraction for the same
	// project. Keyed on the immutable ID; the slug is what's changing.
	unlock := h.projectLocks.Lock(project.ID)
	err = h.projectService.Update(ctx, project, slug)
	unlock()
	if err != nil {
		if errors.Is(err, projects.ErrSlugConflict) {
			http.Error(w, "A project with that slug already exists", http.StatusConflict)
			return
		}
		if errors.Is(err, projects.ErrStorageMissing) {
			http.Error(w, "Cannot rename: this project's documentation directory is missing on disk. "+
				"The rename was not applied. Restart the server to run storage repair, or contact an administrator.",
				http.StatusConflict)
			return
		}
		h.logger.Error("updating project", "error", err)
		http.Error(w, "Failed to update project", http.StatusInternalServerError)
		return
	}

	// When the slug changed, the deployed docs were moved to the new path but
	// the search index still points at the old slug. Refresh it asynchronously
	// (it walks files) so search results link to the new URL.
	if project.Slug != slug {
		h.reindexProjectAsync(project)
	}

	// Flag visibility transitions away from public so the admin sees the
	// "review access" prompt (audit M-3). Editors who had implicit access
	// to a public project lose it the moment the project goes private/custom.
	dest := "/admin/projects"
	if previousVisibility == database.VisibilityPublic && project.Visibility != database.VisibilityPublic {
		dest += "?msg=visibility_restricted"
	}
	h.redirect(w, r, dest, http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if !h.canManage(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Delete search index entries for all versions before deleting project
	if h.searchIndex != nil {
		versions, err := h.versions.ListByProject(ctx, project.ID)
		if err == nil {
			for _, v := range versions {
				if err := h.searchIndex.DeleteVersion(project.ID, v.ID); err != nil {
					h.logger.Error("deleting version from search index", "error", err, "project", slug, "version", v.Tag)
				}
			}
		}
	}

	if err := h.projects.Delete(ctx, project.ID); err != nil {
		h.logger.Error("deleting project", "error", err)
		http.Error(w, "Failed to delete project", http.StatusInternalServerError)
		return
	}

	// Invalidate latest tags cache
	h.invalidateLatestTagsCache()

	h.redirect(w, r, "/admin/projects", http.StatusSeeOther)
}

// currentOrgID flattens the project's nullable org for the edit form, which
// cannot dereference a pointer. 0 means "not set", which the store resolves to
// the default org on save.
func currentOrgID(project *database.Project) int64 {
	if project.OrgID == nil {
		return 0
	}
	return *project.OrgID
}

// exposureForVisibility mirrors the store's transitional mapping so the edit
// form can fall back to it when a caller posts only the legacy field.
func exposureForVisibility(visibility string) string {
	if visibility == database.VisibilityPublic {
		return database.ExposurePublic
	}
	return database.ExposureGranted
}

// handleAdminGrantProjectAccess points a group or a user at this project with
// a role. This is the whole per-project access UI now: one table, one form,
// the same shape as an org's.
func (h *Handler) handleAdminGrantProjectAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	if !h.canManage(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	grant, problem := h.grantFromForm(ctx, r)
	if problem != "" {
		h.projectAccessError(w, r, slug, problem)
		return
	}
	grant.ProjectID = &project.ID

	if err := h.accessGrants.Grant(ctx, grant); err != nil {
		h.logger.Error("granting project access", "project", slug, "error", err)
		h.projectAccessError(w, r, slug, "Could not grant that access.")
		return
	}
	h.redirect(w, r, "/admin/projects/"+slug+"/edit?msg=granted", http.StatusSeeOther)
}

// handleAdminRevokeProjectAccess removes one grant, and says so when the click
// matched nothing rather than redirecting as though it worked (issue #126).
func (h *Handler) handleAdminRevokeProjectAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	if !h.canManage(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	grantID, err := strconv.ParseInt(r.FormValue("grant_id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid grant", http.StatusBadRequest)
		return
	}

	removed, err := h.accessGrants.Revoke(ctx, grantID)
	if err != nil {
		h.logger.Error("revoking project access", "project", slug, "grant_id", grantID, "error", err)
		h.projectAccessError(w, r, slug, "Could not revoke that access.")
		return
	}
	if !removed {
		h.projectAccessError(w, r, slug, "That grant no longer exists.")
		return
	}
	h.redirect(w, r, "/admin/projects/"+slug+"/edit?msg=revoked", http.StatusSeeOther)
}

func (h *Handler) projectAccessError(w http.ResponseWriter, r *http.Request, slug, message string) {
	h.redirect(w, r, "/admin/projects/"+slug+"/edit?msg=error&error="+url.QueryEscape(message), http.StatusSeeOther)
}

// visibilityForExposure keeps the legacy column in step with exposure while
// both exist. A project that is not public keeps whichever narrow visibility
// it already had, since the four old values differ only in which grants
// applied — a distinction the grant table now carries on its own.
func visibilityForExposure(exposure, current string) string {
	if exposure == database.ExposurePublic {
		return database.VisibilityPublic
	}
	if current == "" || current == database.VisibilityPublic {
		return database.VisibilityCustom
	}
	return current
}

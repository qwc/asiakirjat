package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
)

func (h *Handler) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	q := r.URL.Query().Get("q")
	if q == "" {
		h.jsonResponse(w, &docs.SearchResults{Results: []docs.SearchResult{}, Total: 0})
		return
	}

	projectSlug := r.URL.Query().Get("project")
	versionTag := r.URL.Query().Get("version")
	allVersions := r.URL.Query().Get("all_versions") == "1"

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	sq := docs.SearchQuery{
		Query:       q,
		ProjectSlug: projectSlug,
		VersionTag:  versionTag,
		AllVersions: allVersions,
		Limit:       limit,
		Offset:      offset,
	}

	latestTags := h.getLatestVersionTags(ctx)

	results, err := h.searchIndex.Search(sq, latestTags)
	if err != nil {
		h.logger.Error("search failed", "error", err)
		h.jsonError(w, "Search failed", http.StatusInternalServerError)
		return
	}

	// Filter results by user's project access
	results = h.filterSearchResults(ctx, user, results)

	h.jsonResponse(w, results)
}

func (h *Handler) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	q := r.URL.Query().Get("q")
	projectSlug := r.URL.Query().Get("project")
	versionTag := r.URL.Query().Get("version")
	allVersions := r.URL.Query().Get("all_versions") == "1"

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Get all accessible projects for the filter dropdown
	allProjects, _ := h.projects.List(ctx)
	var accessibleProjects []database.Project
	for _, p := range allProjects {
		if h.canViewProject(ctx, user, &p) {
			accessibleProjects = append(accessibleProjects, p)
		}
	}

	// Get versions for selected project
	var projectVersions []string
	if projectSlug != "" {
		project, err := h.projects.GetBySlug(ctx, projectSlug)
		if err == nil {
			versions, _ := h.versions.ListByProject(ctx, project.ID)
			tags := make([]string, len(versions))
			for i, v := range versions {
				tags[i] = v.Tag
			}
			docs.SortVersionTags(tags)
			projectVersions = tags
		}
	}

	data := map[string]any{
		"User":            user,
		"Query":           q,
		"Project":         projectSlug,
		"Version":         versionTag,
		"AllVersions":     allVersions,
		"Limit":           limit,
		"Offset":          offset,
		"Projects":        accessibleProjects,
		"ProjectVersions": projectVersions,
	}

	if q != "" {
		// Determine version filtering:
		// - If no project selected: allVersions checkbox applies
		// - If project selected: version param controls (empty=latest, "all"=all, specific=that version)
		searchAllVersions := allVersions
		searchVersionTag := ""
		if projectSlug != "" {
			searchAllVersions = versionTag == "all"
			if versionTag != "" && versionTag != "all" {
				searchVersionTag = versionTag
			}
		}

		sq := docs.SearchQuery{
			Query:       q,
			ProjectSlug: projectSlug,
			VersionTag:  searchVersionTag,
			AllVersions: searchAllVersions,
			Limit:       limit,
			Offset:      offset,
		}

		latestTags := h.getLatestVersionTags(ctx)

		results, err := h.searchIndex.Search(sq, latestTags)
		if err != nil {
			h.logger.Error("search failed", "error", err)
			data["Error"] = "Search failed"
		} else {
			results = h.filterSearchResults(ctx, user, results)
			data["Results"] = results.Results
			data["Total"] = results.Total
			data["HasPrev"] = offset > 0
			data["HasNext"] = uint64(offset+limit) < results.Total
			data["PrevOffset"] = offset - limit
			data["NextOffset"] = offset + limit
		}
	}

	h.render(w, "search", data)
}

func (h *Handler) handleAdminReindex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check if reindex is already running
	if h.reindexRunning {
		h.redirect(w, r, "/admin/projects?msg=reindex_already_running", http.StatusSeeOther)
		return
	}

	allProjects, err := h.projects.List(ctx)
	if err != nil {
		h.logger.Error("listing projects for reindex", "error", err)
		h.redirect(w, r, "/admin/projects", http.StatusSeeOther)
		return
	}

	var projects []docs.ReindexProject
	var versions []docs.ReindexVersion

	for _, p := range allProjects {
		projects = append(projects, docs.ReindexProject{
			ID:   p.ID,
			Slug: p.Slug,
			Name: p.Name,
		})

		vlist, err := h.versions.ListByProject(ctx, p.ID)
		if err != nil {
			continue
		}
		for _, v := range vlist {
			versions = append(versions, docs.ReindexVersion{
				ID:          v.ID,
				ProjectID:   v.ProjectID,
				Tag:         v.Tag,
				StoragePath: v.StoragePath,
			})
		}
	}

	// Mark reindex as running
	h.reindexRunning = true
	h.reindexProgress = "Starting..."

	go func() {
		defer func() {
			h.reindexRunning = false
			h.reindexProgress = ""
		}()

		progressFn := func(p docs.ReindexProgress) {
			h.reindexProgress = fmt.Sprintf("%d/%d: %s %s", p.Current, p.Total, p.Project, p.Version)
			h.logger.Info("reindex progress", "current", p.Current, "total", p.Total, "project", p.Project, "version", p.Version)
		}

		if err := h.searchIndex.ReindexAllWithProgress(projects, versions, progressFn); err != nil {
			h.logger.Error("reindex failed", "error", err)
		} else {
			h.logger.Info("reindex completed", "versions", len(versions))
		}
	}()

	h.redirect(w, r, "/admin/projects?msg=reindex_started", http.StatusSeeOther)
}

// latestTagsCacheTTL is how long the latest version tags cache is valid.
const latestTagsCacheTTL = 30 * time.Second

// getLatestVersionTags returns a map of projectSlug -> latest version tag.
// Results are cached to avoid per-query DB lookups.
func (h *Handler) getLatestVersionTags(ctx context.Context) map[string]string {
	// Check if cache is still valid
	if h.latestTagsCache != nil && time.Since(h.latestTagsCacheTime) < latestTagsCacheTTL {
		return h.latestTagsCache
	}

	result := make(map[string]string)

	projects, err := h.projects.List(ctx)
	if err != nil {
		return result
	}

	for _, p := range projects {
		versions, err := h.versions.ListByProject(ctx, p.ID)
		if err != nil || len(versions) == 0 {
			continue
		}
		tags := make([]string, len(versions))
		for i, v := range versions {
			tags[i] = v.Tag
		}
		docs.SortVersionTags(tags)
		result[p.Slug] = tags[0]
	}

	// Update cache
	h.latestTagsCache = result
	h.latestTagsCacheTime = time.Now()

	return result
}

// invalidateLatestTagsCache clears the cached latest version tags.
// Call this after uploading or deleting versions.
func (h *Handler) invalidateLatestTagsCache() {
	h.latestTagsCache = nil
}

// filterSearchResults removes results for projects the user can't access
// and prefixes URLs with the base path.
func (h *Handler) filterSearchResults(ctx context.Context, user *database.User, results *docs.SearchResults) *docs.SearchResults {
	// Cache project access checks
	projectCache := make(map[string]bool)
	bp := h.config.Server.BasePath

	var filtered []docs.SearchResult
	for _, r := range results.Results {
		allowed, ok := projectCache[r.ProjectSlug]
		if !ok {
			p, err := h.projects.GetBySlug(ctx, r.ProjectSlug)
			if err != nil {
				allowed = false
			} else {
				allowed = h.canViewProject(ctx, user, p)
			}
			projectCache[r.ProjectSlug] = allowed
		}
		if allowed {
			// Prefix URL with base path
			r.URL = bp + r.URL
			filtered = append(filtered, r)
		}
	}

	if filtered == nil {
		filtered = []docs.SearchResult{}
	}

	return &docs.SearchResults{
		Results: filtered,
		Total:   uint64(len(filtered)),
	}
}

// canViewProject checks if a user can view a project.
func (h *Handler) canViewProject(ctx context.Context, user *database.User, project *database.Project) bool {
	if project.Visibility == database.VisibilityPublic {
		return true
	}
	if user == nil {
		return false
	}
	if user.Role == "admin" {
		return true
	}
	if project.Visibility == database.VisibilityPrivate {
		// Private projects: check global access grants
		if h.globalAccess != nil {
			grant, err := h.globalAccess.GetGrantByUser(ctx, user.ID)
			if err == nil && grant != nil {
				return true
			}
		}
		return false
	}
	// Custom visibility: check project-level access (from all sources: manual, ldap, oauth2)
	effectiveRole, err := h.access.GetEffectiveRole(ctx, project.ID, user.ID)
	return err == nil && effectiveRole != ""
}

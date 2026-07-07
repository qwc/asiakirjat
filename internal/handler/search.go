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

	h.render(w, r, "search", data)
}

func (h *Handler) handleAdminReindex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Claim the slot atomically. tryStart returns false if a reindex is
	// already running.
	if !h.reindex.tryStart() {
		h.redirect(w, r, "/admin/projects?msg=reindex_already_running", http.StatusSeeOther)
		return
	}

	allProjects, err := h.projects.List(ctx)
	if err != nil {
		h.reindex.finish()
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

	h.runJob(func(jobCtx context.Context) {
		defer h.reindex.finish()

		progressFn := func(p docs.ReindexProgress) {
			h.reindex.setProgress(fmt.Sprintf("%d/%d: %s %s", p.Current, p.Total, p.Project, p.Version))
			h.logger.Info("reindex progress", "current", p.Current, "total", p.Total, "project", p.Project, "version", p.Version)
		}

		if err := h.searchIndex.ReindexAllWithProgress(projects, versions, progressFn); err != nil {
			h.logger.Error("reindex failed", "error", err)
		} else {
			h.logger.Info("reindex completed", "versions", len(versions))
		}
	})

	h.redirect(w, r, "/admin/projects?msg=reindex_started", http.StatusSeeOther)
}

// reindexProjectAsync refreshes the search index for a single project's
// versions in the background. It is used after a rename: the indexed
// project_slug and result URLs still carry the old slug, so each version is
// dropped and re-indexed under the new slug and moved storage path. Doc IDs
// are keyed on project/version ID, so this fully replaces the stale entries.
func (h *Handler) reindexProjectAsync(project *database.Project) {
	if h.searchIndex == nil {
		return
	}
	h.runJob(func(ctx context.Context) {
		versions, err := h.versions.ListByProject(ctx, project.ID)
		if err != nil {
			h.logger.Error("listing versions for rename reindex", "project", project.Slug, "error", err)
			return
		}
		for _, v := range versions {
			if err := ctx.Err(); err != nil {
				return
			}
			h.searchIndex.DeleteVersion(project.ID, v.ID)
			if err := h.searchIndex.IndexVersion(project.ID, v.ID, project.Slug, project.Name, v.Tag, v.StoragePath); err != nil {
				h.logger.Error("reindexing version after rename", "project", project.Slug, "version", v.Tag, "error", err)
			}
		}
	})
}

// getLatestVersionTags returns a map of projectSlug -> latest version tag.
// Results are cached to avoid per-query DB lookups; the cache is guarded
// by an internal mutex (see latestTagsCache).
func (h *Handler) getLatestVersionTags(ctx context.Context) map[string]string {
	now := time.Now()
	if cached, ok := h.latestTags.get(now); ok {
		return cached
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
		result[p.Slug] = latestVersionTag(versions, p.PinnedVersion)
	}

	h.latestTags.set(now, result)
	return result
}

// invalidateLatestTagsCache clears the cached latest version tags. Called
// after uploading or deleting versions so the next search sees fresh data.
func (h *Handler) invalidateLatestTagsCache() {
	h.latestTags.invalidate()
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

// canViewProject is a thin forwarder to access.Checker.CanView; the rule
// lives in internal/access.
func (h *Handler) canViewProject(ctx context.Context, user *database.User, project *database.Project) bool {
	return h.checker.CanView(ctx, user, project)
}

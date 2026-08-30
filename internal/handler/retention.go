package handler

import (
	"context"
	"regexp"
	"time"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
)

// effectiveRetentionDays returns the retention policy for a project in days.
// Per-project override takes precedence over the global default.
// Returns 0 for unlimited (no auto-deletion).
func (h *Handler) effectiveRetentionDays(project *database.Project) int {
	if project.RetentionDays != nil {
		return *project.RetentionDays
	}
	return h.config.Retention.NonSemverDays
}

// versionKeeper reports, for one project, whether a version tag is worth
// keeping indefinitely.
//
// A project may name the versions it cares about with a regular expression
// (issue #127) — "^v\\d+\\.\\d+\\.\\d+$" to keep only releases, say, or
// "^(v\\d|stable)" to keep a branch too. Without one, the original rule
// applies: semver-looking tags are kept and everything else expires.
//
// An unparseable pattern falls back to the semver rule rather than matching
// nothing. Retention deletes what it does not keep, so the safe direction on
// a broken pattern is to keep more, not to wipe a project's history. The
// admin UI rejects invalid patterns, so this only covers a value that got
// into the database some other way.
func (h *Handler) versionKeeper(project *database.Project) func(tag string) bool {
	if project.VersionKeepPattern == nil || *project.VersionKeepPattern == "" {
		return docs.IsSemver
	}
	re, err := regexp.Compile(*project.VersionKeepPattern)
	if err != nil {
		h.logger.Error("retention: invalid version keep pattern, falling back to semver",
			"project", project.Slug, "pattern", *project.VersionKeepPattern, "error", err)
		return docs.IsSemver
	}
	return re.MatchString
}

// enforceRetentionPolicy deletes versions the project does not consider worth
// keeping once they are older than its retention period.
func (h *Handler) enforceRetentionPolicy(ctx context.Context, project *database.Project) {
	days := h.effectiveRetentionDays(project)
	if days <= 0 {
		return
	}

	keep := h.versionKeeper(project)

	versions, err := h.versions.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error("retention: listing versions", "error", err, "project", project.Slug)
		return
	}

	cutoff := time.Now().AddDate(0, 0, -days)

	for _, v := range versions {
		if keep(v.Tag) {
			continue
		}
		if v.CreatedAt.After(cutoff) {
			continue
		}

		h.logger.Info("retention: deleting expired version",
			"project", project.Slug, "version", v.Tag,
			"created_at", v.CreatedAt, "retention_days", days)

		if err := h.versions.Delete(ctx, v.ID); err != nil {
			h.logger.Error("retention: deleting version from database", "error", err, "project", project.Slug, "version", v.Tag)
			continue
		}
		if err := h.storage.DeleteVersion(project.Slug, v.Tag); err != nil {
			h.logger.Error("retention: deleting version from filesystem", "error", err, "project", project.Slug, "version", v.Tag)
		}
		if h.searchIndex != nil {
			if err := h.searchIndex.DeleteVersion(project.ID, v.ID); err != nil {
				h.logger.Error("retention: deleting version from search index", "error", err, "project", project.Slug, "version", v.Tag)
			}
		}
		h.invalidateLatestTagsCache()
	}
}

// runRetentionCleanup iterates all projects and enforces retention for
// those with a non-zero effective retention policy.
func (h *Handler) runRetentionCleanup(ctx context.Context) {
	projects, err := h.projects.List(ctx)
	if err != nil {
		h.logger.Error("retention: listing projects", "error", err)
		return
	}

	for i := range projects {
		if ctx.Err() != nil {
			return
		}
		if h.effectiveRetentionDays(&projects[i]) > 0 {
			h.enforceRetentionPolicy(ctx, &projects[i])
		}
	}
}

// StartRetentionWorker runs retention cleanup once immediately, then
// every hour. It stops when the context is cancelled.
func (h *Handler) StartRetentionWorker(ctx context.Context) {
	h.logger.Info("retention worker started")
	h.runRetentionCleanup(ctx)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("retention worker stopped")
			return
		case <-ticker.C:
			h.runRetentionCleanup(ctx)
		}
	}
}

// keepPatternDisplay flattens the project's keep pattern for the edit form,
// which cannot dereference a pointer. Empty means "no pattern set".
func keepPatternDisplay(project *database.Project) string {
	if project.VersionKeepPattern == nil {
		return ""
	}
	return *project.VersionKeepPattern
}

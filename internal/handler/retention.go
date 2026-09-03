package handler

import (
	"context"
	"fmt"
	"math"
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

// effectiveKeepPattern returns the keep pattern that applies to a project and
// where it came from. A project's own pattern wins over the instance default;
// an empty result means neither is set and the built-in "looks version-shaped"
// rule applies.
func (h *Handler) effectiveKeepPattern(project *database.Project) (pattern, source string) {
	if project.VersionKeepPattern != nil && *project.VersionKeepPattern != "" {
		return *project.VersionKeepPattern, "project"
	}
	return h.config.Retention.KeepPattern, "config"
}

// versionKeeper reports, for one project, whether a version tag is worth
// keeping indefinitely.
//
// The rule comes from the project's own pattern when it has one, otherwise
// from retention.keep_pattern, which defaults to keeping release numbers —
// v1.2.3 and 2.0.0 — and expiring everything else (issue #127). A project
// that tags differently sets its own pattern in the admin UI.
//
// An unparseable pattern falls back to keeping anything version-shaped
// (docs.IsSemver) rather than matching nothing. Retention deletes what it
// does not keep, so the safe direction on a broken pattern is to keep more,
// not to wipe a project's history. The admin UI rejects invalid project
// patterns, so this covers a bad value in config or one that reached the
// database another way.
func (h *Handler) versionKeeper(project *database.Project) func(tag string) bool {
	pattern, source := h.effectiveKeepPattern(project)
	if pattern == "" {
		return docs.IsSemver
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		h.logger.Error("retention: invalid version keep pattern, falling back to keeping anything version-shaped",
			"project", project.Slug, "pattern", pattern, "pattern_source", source, "error", err)
		return docs.IsSemver
	}
	return re.MatchString
}

// versionExpiry is the display side of retention for a single version: when
// the cleanup pass will delete it, and how that reads on the project page.
type versionExpiry struct {
	In string // "in 12 days", "in 1 day", or "soon" once the window has passed
	At string // the date the version becomes eligible for deletion
}

// versionExpiries reports which of a project's versions retention will delete
// and when, keyed by version tag. A tag absent from the map is kept.
//
// It mirrors enforceRetentionPolicy deliberately — same keep pattern, same
// pin exemption, same CreatedAt window — so the badge on the project
// page cannot promise something the cleanup pass then contradicts. Returns nil
// when the project has no retention period, which is the shipped default
// (retention.nonsemver_days = 0): nothing expires, so nothing is claimed.
func (h *Handler) versionExpiries(project *database.Project, versions []database.Version) map[string]versionExpiry {
	days := h.effectiveRetentionDays(project)
	if days <= 0 {
		return nil
	}

	keep := h.versionKeeper(project)
	pinned := ""
	if project.PinnedVersion != nil {
		pinned = *project.PinnedVersion
	}

	now := time.Now()
	expiries := make(map[string]versionExpiry)
	for _, v := range versions {
		if keep(v.Tag) || v.Tag == pinned {
			continue
		}
		deleteAt := v.CreatedAt.AddDate(0, 0, days)
		expiries[v.Tag] = versionExpiry{
			In: humanizeDaysLeft(deleteAt.Sub(now)),
			At: deleteAt.Format("2006-01-02"),
		}
	}
	return expiries
}

// humanizeDaysLeft renders the wait until a deletion becomes due. Retention
// runs hourly, so a version already past its window reads "today" rather than
// a negative count: the next pass takes it (issue #157).
func humanizeDaysLeft(d time.Duration) string {
	days := int(math.Ceil(d.Hours() / 24))
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "in 1 day"
	default:
		return fmt.Sprintf("in %d days", days)
	}
}

// enforceRetentionPolicy deletes versions the project does not consider worth
// keeping once they are older than its retention period.
func (h *Handler) enforceRetentionPolicy(ctx context.Context, project *database.Project) {
	days := h.effectiveRetentionDays(project)
	if days <= 0 {
		return
	}

	keep := h.versionKeeper(project)

	// A pin outranks the keep pattern (issue #141). Pinning says "this is the
	// version people should land on", which is the same claim the pattern
	// makes, so deleting a pinned version leaves the pin dangling and silently
	// moves readers to a different version.
	//
	// This holds for a temporary pin too, for as long as it is held (#157).
	// The pin is the point: a temp pin that gets collected out from under the
	// reader is a pin that did nothing. It expires with the pin rather than
	// outliving it — the next upload clears the pin before retention runs, so
	// a version already past its window goes on that same pass.
	pinned := ""
	if project.PinnedVersion != nil {
		pinned = *project.PinnedVersion
	}

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
		if v.Tag == pinned {
			h.logger.Debug("retention: keeping pinned version",
				"project", project.Slug, "version", v.Tag)
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

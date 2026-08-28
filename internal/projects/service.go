// Package projects centralizes project lifecycle operations that previously
// lived in three near-duplicate handlers (admin form, JSON API, auto-create
// on upload). Each handler maps its request to CreateOptions, calls
// Service.Create, and maps the returned typed error to its own HTTP shape.
package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
	"github.com/qwc/asiakirjat/internal/store"
	"github.com/qwc/asiakirjat/internal/validation"
)

// Sentinel errors returned by Service.Create. Callers should use errors.Is.
var (
	ErrInvalidSlug         = errors.New("invalid slug")
	ErrInvalidVisibility   = errors.New("invalid visibility")
	ErrPublicRequiresAdmin = errors.New("only admins can create public projects")
	ErrSlugConflict        = errors.New("project slug already exists")

	// ErrStorageMissing reports that a rename was refused because the
	// project has deployed versions but no directory at its current storage
	// path. Committing the rename would leave the documentation unreachable
	// (issue #129), so Update rolls back instead. Recovering the files is
	// ReconcileStorage's job.
	ErrStorageMissing = errors.New("project storage directory is missing")
)

// CreateOptions is the input to Service.Create. Name defaults to Slug when
// empty; Visibility defaults to VisibilityPrivate when empty.
type CreateOptions struct {
	Slug          string
	Name          string
	Description   string
	Visibility    string
	RetentionDays *int
	Creator       *database.User
}

// Service performs project lifecycle operations. Construct via NewService.
type Service struct {
	projects store.ProjectStore
	versions store.VersionStore
	access   store.ProjectAccessStore
	storage  docs.Storage
	logger   *slog.Logger
}

// NewService wires the dependencies. logger may be nil; in that case slog's
// default logger is used.
func NewService(p store.ProjectStore, v store.VersionStore, a store.ProjectAccessStore, s docs.Storage, l *slog.Logger) *Service {
	if l == nil {
		l = slog.Default()
	}
	return &Service{projects: p, versions: v, access: a, storage: s, logger: l}
}

// Create validates the input, persists the project, ensures its storage
// directory, and auto-grants the creator editor access on non-public projects
// (when Creator is non-admin). Returns the persisted project on success or
// one of the sentinel errors above.
//
// Storage-directory creation failures are non-fatal: the project exists in
// the DB and the directory is created on first upload. The failure is logged.
func (s *Service) Create(ctx context.Context, opts CreateOptions) (*database.Project, error) {
	if !validation.IsValidSlug(opts.Slug) {
		return nil, ErrInvalidSlug
	}
	if opts.Name == "" {
		opts.Name = opts.Slug
	}
	if opts.Visibility == "" {
		opts.Visibility = database.VisibilityPrivate
	}
	if opts.Visibility != database.VisibilityPublic &&
		opts.Visibility != database.VisibilityPrivate &&
		opts.Visibility != database.VisibilityCustom {
		return nil, ErrInvalidVisibility
	}
	if opts.Visibility == database.VisibilityPublic &&
		(opts.Creator == nil || opts.Creator.Role != "admin") {
		return nil, ErrPublicRequiresAdmin
	}

	project := &database.Project{
		Slug:          opts.Slug,
		Name:          opts.Name,
		Description:   opts.Description,
		Visibility:    opts.Visibility,
		RetentionDays: opts.RetentionDays,
	}
	// Record provenance for all creators (admin and non-admin alike) so the
	// manage-projects view can show "created by" and CanManage can grant the
	// creator authority over their own project.
	if opts.Creator != nil {
		project.CreatedBy = &opts.Creator.ID
	}

	if err := s.projects.Create(ctx, project); err != nil {
		if isSlugConflict(err) {
			return nil, ErrSlugConflict
		}
		return nil, fmt.Errorf("creating project: %w", err)
	}

	if err := s.storage.EnsureProjectDir(opts.Slug); err != nil {
		s.logger.Error("creating project directory", "slug", opts.Slug, "error", err)
	}

	if opts.Creator != nil &&
		opts.Creator.Role != "admin" &&
		opts.Visibility != database.VisibilityPublic {
		grant := &database.ProjectAccess{
			ProjectID: project.ID,
			UserID:    opts.Creator.ID,
			Role:      "editor",
		}
		if err := s.access.Grant(ctx, grant); err != nil {
			s.logger.Error("auto-granting creator access", "slug", opts.Slug, "user", opts.Creator.Username, "error", err)
		}
	}

	return project, nil
}

// Update persists edits to an existing project and, when the slug changed,
// migrates its deployed documentation so URLs keep resolving. The caller is
// responsible for having already applied field changes to project and for
// passing the project's previous slug as oldSlug.
//
// Storage and the search index are keyed on the slug, so a bare DB update
// leaves the files stranded at the old-slug path (the rename bug). Here we:
//  1. persist the row (mapping a slug collision to ErrSlugConflict);
//  2. os.Rename the on-disk project directory to the new slug;
//  3. rewrite each version's stored path to match.
//
// If the storage move fails we roll the slug back in the DB so the project
// stays reachable at its old URL rather than pointing at files that never
// moved. The search index is refreshed by the caller (it walks files and is
// best-effort/async), not here.
//
// Callers must serialize Update against concurrent uploads for the same
// project (both mutate the project directory) — the handler holds a
// per-project lock around this call and around archive extraction.
func (s *Service) Update(ctx context.Context, project *database.Project, oldSlug string) error {
	if err := s.projects.Update(ctx, project); err != nil {
		if isSlugConflict(err) {
			return ErrSlugConflict
		}
		return fmt.Errorf("updating project: %w", err)
	}

	if project.Slug == oldSlug {
		return nil
	}

	if err := s.storage.MoveProject(oldSlug, project.Slug); err != nil {
		// A missing source directory is only benign when the project has
		// nothing deployed; then there is genuinely nothing to move and the
		// rename is safe to keep. With versions on record the files should
		// have been there, so treat it as corruption rather than success —
		// silently committing here is what produced issue #129.
		if errors.Is(err, docs.ErrNoSourceDir) && !s.hasVersions(ctx, project.ID) {
			return nil
		}

		// Roll the slug back so the project keeps resolving to its existing
		// files instead of a path that was never populated.
		project.Slug = oldSlug
		if rbErr := s.projects.Update(ctx, project); rbErr != nil {
			s.logger.Error("rolling back slug after failed storage move",
				"project", project.ID, "error", rbErr)
		}
		if errors.Is(err, docs.ErrNoSourceDir) {
			s.logger.Error("refusing rename: project has versions but no storage directory",
				"project", project.ID, "slug", oldSlug,
				"expected_path", s.storage.ProjectPath(oldSlug))
			return fmt.Errorf("%w: %s", ErrStorageMissing, oldSlug)
		}
		return fmt.Errorf("moving project storage: %w", err)
	}

	// Point version records at the new location. Serving recomputes the path
	// from the slug and doesn't consult StoragePath, so a failure here only
	// affects reindexing — log and carry on rather than fail the rename.
	if err := s.rewriteVersionPaths(ctx, project); err != nil {
		s.logger.Error("rewriting version storage paths after rename",
			"project", project.ID, "slug", project.Slug, "error", err)
	}

	return nil
}

// hasVersions reports whether the project has any version rows. A store
// failure is reported as "has versions" so an unreadable database makes
// Update refuse the rename rather than commit a possibly-breaking one.
func (s *Service) hasVersions(ctx context.Context, projectID int64) bool {
	versions, err := s.versions.ListByProject(ctx, projectID)
	if err != nil {
		s.logger.Error("listing versions while checking storage",
			"project", projectID, "error", err)
		return true
	}
	return len(versions) > 0
}

// rewriteVersionPaths updates every version's StoragePath to reflect the
// project's current slug after a rename.
func (s *Service) rewriteVersionPaths(ctx context.Context, project *database.Project) error {
	versions, err := s.versions.ListByProject(ctx, project.ID)
	if err != nil {
		return fmt.Errorf("listing versions: %w", err)
	}
	for i := range versions {
		v := &versions[i]
		v.StoragePath = s.storage.VersionPath(project.Slug, v.Tag)
		if err := s.versions.Update(ctx, v); err != nil {
			return fmt.Errorf("updating version %d: %w", v.ID, err)
		}
	}
	return nil
}

// isSlugConflict reports whether err is a unique-constraint violation on the
// slug column. Error strings differ by dialect; match the two we see
// (SQLite "UNIQUE", postgres/mysql "duplicate").
func isSlugConflict(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "duplicate")
}

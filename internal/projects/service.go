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
	access   store.ProjectAccessStore
	storage  docs.Storage
	logger   *slog.Logger
}

// NewService wires the dependencies. logger may be nil; in that case slog's
// default logger is used.
func NewService(p store.ProjectStore, a store.ProjectAccessStore, s docs.Storage, l *slog.Logger) *Service {
	if l == nil {
		l = slog.Default()
	}
	return &Service{projects: p, access: a, storage: s, logger: l}
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
		// Unique-violation error strings differ by dialect; match the two we
		// see (SQLite "UNIQUE", postgres/mysql "duplicate").
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "duplicate") {
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

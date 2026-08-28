package projects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qwc/asiakirjat/internal/database"
)

// ReconcileStorage repairs projects whose documentation directory no longer
// sits where their slug says it should.
//
// Renames performed before the fix for issue #122 updated the database but
// never moved the files, leaving the slug and the on-disk directory divorced.
// Because serving recomputes the path from the slug, those projects 404 even
// though their files are intact. Version rows still carry the StoragePath
// recorded at upload time, which points at the real location, so that is the
// breadcrumb used to find the files again.
//
// A project is only touched when all of these hold: it has deployed versions,
// nothing exists at its expected path, and its versions agree on a single
// existing source directory that is a direct child of the storage root. That
// makes the repair a no-op on healthy installations. Returns the number of
// projects repaired; individual failures are logged and skipped so one bad
// project cannot stop startup.
func (s *Service) ReconcileStorage(ctx context.Context) (int, error) {
	allProjects, err := s.projects.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing projects: %w", err)
	}

	repaired := 0
	for i := range allProjects {
		p := &allProjects[i]
		source, ok := s.divorcedSource(ctx, p)
		if !ok {
			continue
		}

		// Reuse MoveProject so the destination-exists guard applies here too.
		oldSlug := filepath.Base(source)
		if err := s.storage.MoveProject(oldSlug, p.Slug); err != nil {
			s.logger.Error("reconciling project storage",
				"project", p.ID, "slug", p.Slug, "from", source, "error", err)
			continue
		}

		if err := s.rewriteVersionPaths(ctx, p); err != nil {
			// The files are in the right place now, which is what serving
			// depends on; stale StoragePath values only affect reindexing.
			s.logger.Error("rewriting version paths after reconcile",
				"project", p.ID, "slug", p.Slug, "error", err)
		}

		s.logger.Warn("repaired project storage left behind by an older rename",
			"project", p.ID, "slug", p.Slug,
			"moved_from", source, "moved_to", s.storage.ProjectPath(p.Slug))
		repaired++
	}

	return repaired, nil
}

// divorcedSource returns the directory a project's files actually live in when
// that directory disagrees with the project's slug, and whether a repair
// should be attempted at all.
func (s *Service) divorcedSource(ctx context.Context, p *database.Project) (string, bool) {
	versions, err := s.versions.ListByProject(ctx, p.ID)
	if err != nil {
		s.logger.Error("listing versions while reconciling",
			"project", p.ID, "error", err)
		return "", false
	}
	if len(versions) == 0 {
		return "", false // nothing deployed, nothing to repair
	}

	expected := s.storage.ProjectPath(p.Slug)
	if _, err := os.Stat(expected); err == nil {
		return "", false // healthy
	} else if !os.IsNotExist(err) {
		s.logger.Error("checking project directory while reconciling",
			"project", p.ID, "path", expected, "error", err)
		return "", false
	}

	// Every version must point at the same project directory; a split set
	// means something we do not understand, so leave it for a human.
	source := ""
	for _, v := range versions {
		if v.StoragePath == "" {
			return "", false
		}
		dir := filepath.Dir(filepath.Clean(v.StoragePath))
		if source == "" {
			source = dir
		} else if dir != source {
			s.logger.Warn("skipping reconcile: versions disagree on storage location",
				"project", p.ID, "slug", p.Slug, "a", source, "b", dir)
			return "", false
		}
	}

	// Only ever move a direct child of the storage root, and never onto
	// itself. This keeps a corrupt StoragePath from relocating something
	// outside the docs tree.
	if source == "" || source == expected || filepath.Dir(source) != filepath.Clean(s.storage.BasePath()) {
		return "", false
	}
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return "", false // the breadcrumb leads nowhere; nothing we can do
	}

	return source, true
}

package docs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoSourceDir reports that MoveProject found nothing to move: the source
// project directory does not exist. Callers must decide whether that is
// benign (a project that was never deployed) or a corruption signal (the
// project has deployed versions, so its files should have been there).
// Returning success here is what let a rename commit in the database while
// the documentation stayed behind at its old path — see issue #129.
var ErrNoSourceDir = errors.New("source project directory does not exist")

type Storage interface {
	BasePath() string
	ProjectPath(slug string) string
	VersionPath(slug, tag string) string
	EnsureProjectDir(slug string) error
	EnsureVersionDir(slug, tag string) error
	VersionExists(slug, tag string) bool
	DeleteVersion(slug, tag string) error
	MoveProject(oldSlug, newSlug string) error
}

type FilesystemStorage struct {
	basePath string
}

func NewFilesystemStorage(basePath string) *FilesystemStorage {
	return &FilesystemStorage{basePath: basePath}
}

func (s *FilesystemStorage) BasePath() string {
	return s.basePath
}

func (s *FilesystemStorage) ProjectPath(slug string) string {
	return filepath.Join(s.basePath, slug)
}

func (s *FilesystemStorage) VersionPath(slug, tag string) string {
	return filepath.Join(s.basePath, slug, tag)
}

func (s *FilesystemStorage) EnsureProjectDir(slug string) error {
	path := s.ProjectPath(slug)
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("creating project directory: %w", err)
	}
	return nil
}

func (s *FilesystemStorage) EnsureVersionDir(slug, tag string) error {
	path := s.VersionPath(slug, tag)
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("creating version directory: %w", err)
	}
	return nil
}

func (s *FilesystemStorage) VersionExists(slug, tag string) bool {
	path := s.VersionPath(slug, tag)
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (s *FilesystemStorage) DeleteVersion(slug, tag string) error {
	path := s.VersionPath(slug, tag)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("deleting version directory: %w", err)
	}
	return nil
}

// MoveProject relocates a project's deployed documentation from oldSlug to
// newSlug when the project is renamed. Storage is keyed on slug, so without
// this the files would be orphaned at the old path and every doc URL would
// 404 (see the rename bug). It is a no-op if the old directory does not exist
// yet (a project can be renamed before anything is deployed). It refuses to
// overwrite an existing destination so a rename can never clobber another
// project's docs.
func (s *FilesystemStorage) MoveProject(oldSlug, newSlug string) error {
	if oldSlug == newSlug {
		return nil
	}
	oldPath := s.ProjectPath(oldSlug)
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return fmt.Errorf("moving project %q: %w", oldSlug, ErrNoSourceDir)
	} else if err != nil {
		return fmt.Errorf("checking project directory: %w", err)
	}
	newPath := s.ProjectPath(newSlug)
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("destination project directory already exists: %s", newSlug)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking destination directory: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("moving project directory: %w", err)
	}
	return nil
}

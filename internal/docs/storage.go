package docs

import (
	"fmt"
	"os"
	"path/filepath"
)

type Storage interface {
	BasePath() string
	ProjectPath(slug string) string
	VersionPath(slug, tag string) string
	EnsureProjectDir(slug string) error
	EnsureVersionDir(slug, tag string) error
	VersionExists(slug, tag string) bool
	DeleteVersion(slug, tag string) error
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

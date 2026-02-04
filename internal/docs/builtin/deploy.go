package builtin

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
	"github.com/qwc/asiakirjat/internal/store"
)

const (
	// ProjectSlug is the slug used for the built-in docs project.
	ProjectSlug = "asiakirjat-docs"
	// ProjectName is the display name for the built-in docs project.
	ProjectName = "Asiakirjat Documentation"
	// ProjectDescription is the description for the built-in docs project.
	ProjectDescription = "Built-in documentation for Asiakirjat"
)

// Deployer handles deployment of built-in documentation.
type Deployer struct {
	Storage     docs.Storage
	Projects    store.ProjectStore
	Versions    store.VersionStore
	SearchIndex *docs.SearchIndex
	BasePath    string // URL base path (e.g., "/docs")
	Logger      *slog.Logger
}

// Deploy creates or updates the built-in documentation project.
func (d *Deployer) Deploy(ctx context.Context, userID int64) error {
	// Validate docs exist
	if err := ValidateDocs(); err != nil {
		return fmt.Errorf("validating docs: %w", err)
	}

	// Get or create the project
	project, err := d.ensureProject(ctx)
	if err != nil {
		return fmt.Errorf("ensuring project: %w", err)
	}

	// Use app version as version tag
	versionTag := Version
	if versionTag == "" {
		versionTag = "dev"
	}

	// Check if this version already exists
	existingVersion, err := d.Versions.GetByProjectAndTag(ctx, project.ID, versionTag)
	if err == nil && existingVersion != nil {
		// Delete existing version
		d.Logger.Info("deleting existing built-in docs version", "version", versionTag)

		// Delete from search index
		if d.SearchIndex != nil {
			if err := d.SearchIndex.DeleteVersion(project.ID, existingVersion.ID); err != nil {
				d.Logger.Error("deleting version from search index", "error", err)
			}
		}

		// Delete version files
		if err := d.Storage.DeleteVersion(ProjectSlug, versionTag); err != nil {
			d.Logger.Error("deleting version files", "error", err)
		}

		// Delete version record
		if err := d.Versions.Delete(ctx, existingVersion.ID); err != nil {
			return fmt.Errorf("deleting existing version: %w", err)
		}
	}

	// Create version directory
	if err := d.Storage.EnsureVersionDir(ProjectSlug, versionTag); err != nil {
		return fmt.Errorf("creating version directory: %w", err)
	}

	// Parse documentation tree for navigation
	navTree, err := ParseDocTree()
	if err != nil {
		return fmt.Errorf("parsing doc tree: %w", err)
	}

	// Convert and write all markdown files
	storagePath := d.Storage.VersionPath(ProjectSlug, versionTag)
	if err := d.convertAndWriteDocs(navTree, storagePath); err != nil {
		// Clean up on failure
		d.Storage.DeleteVersion(ProjectSlug, versionTag)
		return fmt.Errorf("converting docs: %w", err)
	}

	// Create version record
	version := &database.Version{
		ProjectID:   project.ID,
		Tag:         versionTag,
		StoragePath: storagePath,
		UploadedBy:  userID,
	}
	if err := d.Versions.Create(ctx, version); err != nil {
		// Clean up on failure
		d.Storage.DeleteVersion(ProjectSlug, versionTag)
		return fmt.Errorf("creating version record: %w", err)
	}

	// Index for search
	if d.SearchIndex != nil {
		go func() {
			if err := d.SearchIndex.IndexVersion(
				project.ID, version.ID,
				project.Slug, project.Name,
				versionTag, storagePath,
			); err != nil {
				d.Logger.Error("indexing built-in docs", "error", err)
			} else {
				d.Logger.Info("indexed built-in docs for search", "version", versionTag)
			}
		}()
	}

	d.Logger.Info("deployed built-in documentation",
		"project", ProjectSlug,
		"version", versionTag,
	)

	return nil
}

// ensureProject creates the docs project if it doesn't exist.
func (d *Deployer) ensureProject(ctx context.Context) (*database.Project, error) {
	// Try to get existing project
	project, err := d.Projects.GetBySlug(ctx, ProjectSlug)
	if err == nil {
		return project, nil
	}

	// Create new project
	project = &database.Project{
		Slug:        ProjectSlug,
		Name:        ProjectName,
		Description: ProjectDescription,
		Visibility:  database.VisibilityPublic, // Built-in docs are public
	}

	if err := d.Projects.Create(ctx, project); err != nil {
		return nil, err
	}

	if err := d.Storage.EnsureProjectDir(ProjectSlug); err != nil {
		return nil, err
	}

	d.Logger.Info("created built-in docs project", "slug", ProjectSlug)
	return project, nil
}

// convertAndWriteDocs converts all markdown files to HTML and writes them.
func (d *Deployer) convertAndWriteDocs(navTree []DocEntry, storagePath string) error {
	docsRoot := GetDocsFS()

	return fs.WalkDir(docsRoot, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			// Create directory in output
			outDir := filepath.Join(storagePath, path)
			if err := os.MkdirAll(outDir, 0755); err != nil {
				return fmt.Errorf("creating directory %s: %w", outDir, err)
			}
			return nil
		}

		// Skip non-markdown files
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		// Read markdown
		content, err := fs.ReadFile(docsRoot, path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		// Determine current path for navigation highlighting
		currentPath := strings.TrimSuffix(path, ".md")

		// Convert to HTML
		htmlContent, err := ConvertToHTML(content, navTree, currentPath, d.BasePath)
		if err != nil {
			return fmt.Errorf("converting %s: %w", path, err)
		}

		// Write HTML file
		htmlPath := strings.TrimSuffix(path, ".md") + ".html"
		outPath := filepath.Join(storagePath, htmlPath)

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return fmt.Errorf("creating parent directory for %s: %w", outPath, err)
		}

		if err := os.WriteFile(outPath, htmlContent, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}

		return nil
	})
}

// IsBuiltinDocsProject returns true if the given slug is the built-in docs project.
func IsBuiltinDocsProject(slug string) bool {
	return slug == ProjectSlug
}

// GetProjectSlug returns the slug for the built-in docs project.
func GetProjectSlug() string {
	return ProjectSlug
}

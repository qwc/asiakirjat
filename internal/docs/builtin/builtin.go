// Package builtin provides embedded documentation that can be deployed as a regular project.
package builtin

import (
	"bytes"
	"embed"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed all:docs
var docsFS embed.FS

// Version is the application version, set from main.go via ldflags.
var Version = "dev"

// DocEntry represents a documentation file or directory.
type DocEntry struct {
	Title    string     // Display title (from # heading or filename)
	Path     string     // Relative path without extension (e.g., "tutorials/getting-started")
	HTMLPath string     // Path with .html extension
	Children []DocEntry // Subdirectory entries (for directories)
	IsDir    bool       // Whether this is a directory
	Order    int        // Sort order for display
}

// sectionOrder defines the display order for top-level sections.
var sectionOrder = map[string]int{
	"tutorials":   1,
	"how-to":      2,
	"reference":   3,
	"explanation": 4,
}

// sectionTitles provides human-readable titles for sections.
var sectionTitles = map[string]string{
	"tutorials":   "Tutorials",
	"how-to":      "How-To Guides",
	"reference":   "Reference",
	"explanation": "Explanation",
}

// GetDocsFS returns the embedded documentation filesystem.
func GetDocsFS() fs.FS {
	sub, _ := fs.Sub(docsFS, "docs")
	return sub
}

// ParseDocTree parses the embedded docs directory and returns a structured tree.
func ParseDocTree() ([]DocEntry, error) {
	docsRoot, err := fs.Sub(docsFS, "docs")
	if err != nil {
		return nil, err
	}

	var entries []DocEntry

	// Read top-level directories
	topLevel, err := fs.ReadDir(docsRoot, ".")
	if err != nil {
		return nil, err
	}

	for _, entry := range topLevel {
		if entry.IsDir() {
			// This is a section directory (tutorials, how-to, etc.)
			section := DocEntry{
				Title:   sectionTitles[entry.Name()],
				Path:    entry.Name(),
				IsDir:   true,
				Order:   sectionOrder[entry.Name()],
				Children: []DocEntry{},
			}

			if section.Title == "" {
				section.Title = titleFromFilename(entry.Name())
			}

			// Read files in section
			sectionPath := entry.Name()
			sectionFiles, err := fs.ReadDir(docsRoot, sectionPath)
			if err != nil {
				continue
			}

			for _, f := range sectionFiles {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
					continue
				}

				mdPath := filepath.Join(sectionPath, f.Name())
				content, err := fs.ReadFile(docsRoot, mdPath)
				if err != nil {
					continue
				}

				baseName := strings.TrimSuffix(f.Name(), ".md")
				child := DocEntry{
					Title:    GetTitle(content, baseName),
					Path:     filepath.Join(sectionPath, baseName),
					HTMLPath: filepath.Join(sectionPath, baseName+".html"),
					IsDir:    false,
				}
				section.Children = append(section.Children, child)
			}

			// Sort children alphabetically by title
			sort.Slice(section.Children, func(i, j int) bool {
				return section.Children[i].Title < section.Children[j].Title
			})

			entries = append(entries, section)
		} else if strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "index.md" {
			// Top-level non-index markdown file
			content, err := fs.ReadFile(docsRoot, entry.Name())
			if err != nil {
				continue
			}

			baseName := strings.TrimSuffix(entry.Name(), ".md")
			entries = append(entries, DocEntry{
				Title:    GetTitle(content, baseName),
				Path:     baseName,
				HTMLPath: baseName + ".html",
				IsDir:    false,
				Order:    0,
			})
		}
	}

	// Sort entries by order, then alphabetically
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Order != entries[j].Order {
			return entries[i].Order < entries[j].Order
		}
		return entries[i].Title < entries[j].Title
	})

	return entries, nil
}

// titleRegex matches the first # heading in a markdown file.
var titleRegex = regexp.MustCompile(`(?m)^#\s+(.+)$`)

// GetTitle extracts the title from markdown content.
// It looks for the first # heading, or falls back to the filename.
func GetTitle(content []byte, fallback string) string {
	matches := titleRegex.FindSubmatch(content)
	if len(matches) >= 2 {
		return strings.TrimSpace(string(matches[1]))
	}
	return titleFromFilename(fallback)
}

// titleFromFilename converts a filename to a title.
func titleFromFilename(name string) string {
	name = strings.TrimSuffix(name, ".md")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")

	// Title case
	words := strings.Fields(name)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// ReadMarkdown reads a markdown file from the embedded docs.
func ReadMarkdown(path string) ([]byte, error) {
	docsRoot, err := fs.Sub(docsFS, "docs")
	if err != nil {
		return nil, err
	}

	// Handle index.html -> index.md
	if path == "" || path == "index.html" {
		path = "index.md"
	}

	// Convert .html to .md
	if strings.HasSuffix(path, ".html") {
		path = strings.TrimSuffix(path, ".html") + ".md"
	}

	// Read the file
	content, err := fs.ReadFile(docsRoot, path)
	if err != nil {
		return nil, err
	}

	return content, nil
}

// ListMarkdownFiles returns all markdown file paths in the docs.
func ListMarkdownFiles() ([]string, error) {
	docsRoot, err := fs.Sub(docsFS, "docs")
	if err != nil {
		return nil, err
	}

	var files []string
	err = fs.WalkDir(docsRoot, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

// GetIndexContent returns the index.md content.
func GetIndexContent() ([]byte, error) {
	docsRoot, err := fs.Sub(docsFS, "docs")
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(docsRoot, "index.md")
}

// TransformMarkdownLinks converts .md links to .html links in content.
func TransformMarkdownLinks(content []byte) []byte {
	// Replace relative .md links with .html
	mdLinkRegex := regexp.MustCompile(`\]\(([^)]+)\.md\)`)
	return mdLinkRegex.ReplaceAll(content, []byte(`]($1.html)`))
}

// HasFile checks if a file exists in the embedded docs.
func HasFile(path string) bool {
	docsRoot, err := fs.Sub(docsFS, "docs")
	if err != nil {
		return false
	}

	// Convert .html to .md for checking
	if strings.HasSuffix(path, ".html") {
		path = strings.TrimSuffix(path, ".html") + ".md"
	}

	_, err = fs.Stat(docsRoot, path)
	return err == nil
}

// MustReadFile reads a file and panics if it doesn't exist.
// Only use during initialization.
func MustReadFile(path string) []byte {
	content, err := ReadMarkdown(path)
	if err != nil {
		panic("builtin: missing required file: " + path + ": " + err.Error())
	}
	return content
}

// ValidateDocs checks that all required documentation files exist.
func ValidateDocs() error {
	required := []string{
		"index.md",
		"tutorials/getting-started.md",
		"tutorials/first-project.md",
		"tutorials/uploading-docs.md",
		"how-to/configure-ldap.md",
		"how-to/configure-oauth2.md",
		"how-to/api-tokens.md",
		"how-to/ci-cd-integration.md",
		"reference/configuration.md",
		"reference/api.md",
		"reference/roles-permissions.md",
		"reference/archive-formats.md",
		"explanation/architecture.md",
		"explanation/authentication.md",
		"explanation/search-indexing.md",
	}

	docsRoot, err := fs.Sub(docsFS, "docs")
	if err != nil {
		return err
	}

	var missing []string
	for _, path := range required {
		if _, err := fs.Stat(docsRoot, path); err != nil {
			missing = append(missing, path)
		}
	}

	if len(missing) > 0 {
		var buf bytes.Buffer
		buf.WriteString("missing required documentation files:\n")
		for _, m := range missing {
			buf.WriteString("  - ")
			buf.WriteString(m)
			buf.WriteString("\n")
		}
		return &MissingDocsError{Missing: missing}
	}

	return nil
}

// MissingDocsError is returned when required docs are missing.
type MissingDocsError struct {
	Missing []string
}

func (e *MissingDocsError) Error() string {
	return "missing required documentation files"
}

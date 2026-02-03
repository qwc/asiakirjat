package builtin

import (
	"strings"
	"testing"
)

func TestValidateDocs(t *testing.T) {
	err := ValidateDocs()
	if err != nil {
		t.Errorf("ValidateDocs() error = %v", err)
	}
}

func TestParseDocTree(t *testing.T) {
	entries, err := ParseDocTree()
	if err != nil {
		t.Fatalf("ParseDocTree() error = %v", err)
	}

	if len(entries) == 0 {
		t.Error("ParseDocTree() returned no entries")
	}

	// Check that we have the expected sections
	sectionNames := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir {
			sectionNames[e.Title] = true
		}
	}

	expectedSections := []string{"Tutorials", "How-To Guides", "Reference", "Explanation"}
	for _, name := range expectedSections {
		if !sectionNames[name] {
			t.Errorf("ParseDocTree() missing section %q", name)
		}
	}
}

func TestGetTitle(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		fallback string
		want     string
	}{
		{
			name:     "heading at start",
			content:  "# My Title\n\nSome content",
			fallback: "fallback",
			want:     "My Title",
		},
		{
			name:     "heading with whitespace",
			content:  "\n\n# Another Title\n\nContent",
			fallback: "fallback",
			want:     "Another Title",
		},
		{
			name:     "no heading",
			content:  "Just some content without a heading",
			fallback: "my-fallback",
			want:     "My Fallback",
		},
		{
			name:     "empty content",
			content:  "",
			fallback: "test-file",
			want:     "Test File",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetTitle([]byte(tt.content), tt.fallback)
			if got != tt.want {
				t.Errorf("GetTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadMarkdown(t *testing.T) {
	content, err := ReadMarkdown("index.md")
	if err != nil {
		t.Fatalf("ReadMarkdown(index.md) error = %v", err)
	}

	if len(content) == 0 {
		t.Error("ReadMarkdown(index.md) returned empty content")
	}

	// Check it contains expected content
	if !strings.Contains(string(content), "Asiakirjat") {
		t.Error("ReadMarkdown(index.md) doesn't contain expected 'Asiakirjat' text")
	}
}

func TestListMarkdownFiles(t *testing.T) {
	files, err := ListMarkdownFiles()
	if err != nil {
		t.Fatalf("ListMarkdownFiles() error = %v", err)
	}

	if len(files) < 15 {
		t.Errorf("ListMarkdownFiles() returned %d files, expected at least 15", len(files))
	}

	// Check for expected files
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}

	expected := []string{
		"index.md",
		"tutorials/getting-started.md",
		"how-to/configure-ldap.md",
		"reference/configuration.md",
		"explanation/architecture.md",
	}

	for _, exp := range expected {
		if !fileSet[exp] {
			t.Errorf("ListMarkdownFiles() missing expected file %q", exp)
		}
	}
}

func TestTransformMarkdownLinks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple link",
			input: "[Link](file.md)",
			want:  "[Link](file.html)",
		},
		{
			name:  "relative link",
			input: "[Link](../reference/api.md)",
			want:  "[Link](../reference/api.html)",
		},
		{
			name:  "multiple links",
			input: "[One](a.md) and [Two](b.md)",
			want:  "[One](a.html) and [Two](b.html)",
		},
		{
			name:  "no change for html",
			input: "[Link](file.html)",
			want:  "[Link](file.html)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(TransformMarkdownLinks([]byte(tt.input)))
			if got != tt.want {
				t.Errorf("TransformMarkdownLinks() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"index.md", true},
		{"index.html", true}, // converted
		{"tutorials/getting-started.md", true},
		{"tutorials/getting-started.html", true}, // converted
		{"nonexistent.md", false},
		{"tutorials/nonexistent.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := HasFile(tt.path)
			if got != tt.want {
				t.Errorf("HasFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestConvertToHTML(t *testing.T) {
	// Get nav tree
	nav, err := ParseDocTree()
	if err != nil {
		t.Fatalf("ParseDocTree() error = %v", err)
	}

	content := []byte("# Test Page\n\nSome content here.")
	result, err := ConvertToHTML(content, nav, "test", "/basepath")
	if err != nil {
		t.Fatalf("ConvertToHTML() error = %v", err)
	}

	html := string(result)

	// Check it's valid HTML
	if !strings.Contains(html, "<!DOCTYPE html>") {
		t.Error("ConvertToHTML() missing DOCTYPE")
	}

	if !strings.Contains(html, "<title>Test Page") {
		t.Error("ConvertToHTML() missing title")
	}

	if !strings.Contains(html, "Some content here") {
		t.Error("ConvertToHTML() missing content")
	}

	if !strings.Contains(html, "doc-sidebar") {
		t.Error("ConvertToHTML() missing sidebar")
	}

	if !strings.Contains(html, "/basepath/") {
		t.Error("ConvertToHTML() missing base path")
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "index"},
		{"index", "index"},
		{"index.html", "index"},
		{"index.md", "index"},
		{"/index.html", "index"},
		{"tutorials/getting-started", "tutorials/getting-started"},
		{"tutorials/getting-started.html", "tutorials/getting-started"},
		{"/tutorials/getting-started.md", "tutorials/getting-started"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizePath(tt.input)
			if got != tt.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

package builtin

import (
	"bytes"
	"fmt"
	"html/template"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// docTemplate is the HTML template for rendered documentation pages.
const docTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}} - Asiakirjat Documentation</title>
    <style>
        * { box-sizing: border-box; }
        body {
            margin: 0;
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            line-height: 1.6;
            color: #1a1a2e;
            background: #f8fafc;
        }
        .doc-header {
            background: #1e293b;
            color: white;
            padding: 0.75rem 1.5rem;
            position: sticky;
            top: 0;
            z-index: 100;
            display: flex;
            align-items: center;
            justify-content: space-between;
        }
        .doc-header a {
            color: #94a3b8;
            text-decoration: none;
        }
        .doc-header a:hover {
            color: #e2e8f0;
        }
        .doc-header-title {
            font-weight: 600;
            color: white;
        }
        .doc-layout {
            display: flex;
            min-height: calc(100vh - 52px);
        }
        .doc-sidebar {
            width: 280px;
            background: #1e293b;
            padding: 1rem;
            position: sticky;
            top: 52px;
            height: calc(100vh - 52px);
            overflow-y: auto;
            flex-shrink: 0;
        }
        .doc-sidebar a {
            color: #94a3b8;
            text-decoration: none;
            display: block;
            padding: 0.5rem 0.75rem;
            border-radius: 4px;
            font-size: 0.9rem;
        }
        .doc-sidebar a:hover {
            background: #334155;
            color: #e2e8f0;
        }
        .doc-sidebar a.active {
            background: #3b82f6;
            color: white;
        }
        .doc-section {
            margin-top: 1.5rem;
        }
        .doc-section:first-child {
            margin-top: 0;
        }
        .doc-section-title {
            color: #64748b;
            font-size: 0.7rem;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            padding: 0 0.75rem;
            margin-bottom: 0.5rem;
            font-weight: 600;
        }
        .doc-content {
            flex: 1;
            padding: 2rem 3rem;
            max-width: 900px;
            min-width: 0;
        }
        .doc-content h1 {
            margin-top: 0;
            padding-bottom: 0.5rem;
            border-bottom: 1px solid #e2e8f0;
            font-size: 2rem;
        }
        .doc-content h2 {
            margin-top: 2rem;
            font-size: 1.5rem;
            color: #334155;
        }
        .doc-content h3 {
            margin-top: 1.5rem;
            font-size: 1.25rem;
            color: #475569;
        }
        .doc-content a {
            color: #3b82f6;
            text-decoration: none;
        }
        .doc-content a:hover {
            text-decoration: underline;
        }
        .doc-content code {
            font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', monospace;
            font-size: 0.875em;
            background: #e2e8f0;
            padding: 0.125rem 0.375rem;
            border-radius: 3px;
        }
        .doc-content pre {
            background: #1e293b;
            color: #e2e8f0;
            padding: 1rem;
            border-radius: 6px;
            overflow-x: auto;
            font-size: 0.875rem;
            line-height: 1.5;
        }
        .doc-content pre code {
            background: none;
            padding: 0;
            font-size: inherit;
        }
        .doc-content table {
            width: 100%;
            border-collapse: collapse;
            margin: 1rem 0;
        }
        .doc-content th,
        .doc-content td {
            border: 1px solid #e2e8f0;
            padding: 0.75rem;
            text-align: left;
        }
        .doc-content th {
            background: #f1f5f9;
            font-weight: 600;
        }
        .doc-content blockquote {
            margin: 1rem 0;
            padding: 0.75rem 1rem;
            border-left: 4px solid #3b82f6;
            background: #f1f5f9;
        }
        .doc-content blockquote p {
            margin: 0;
        }
        .doc-content ul, .doc-content ol {
            padding-left: 1.5rem;
        }
        .doc-content li {
            margin: 0.25rem 0;
        }
        .doc-content hr {
            border: none;
            border-top: 1px solid #e2e8f0;
            margin: 2rem 0;
        }
        @media (max-width: 768px) {
            .doc-layout {
                flex-direction: column;
            }
            .doc-sidebar {
                width: 100%;
                height: auto;
                position: static;
                max-height: 50vh;
            }
            .doc-content {
                padding: 1.5rem;
            }
        }
    </style>
</head>
<body>
    <header class="doc-header">
        <span class="doc-header-title">Asiakirjat Documentation</span>
        <a href="{{.BasePath}}/">Back to Projects</a>
    </header>
    <div class="doc-layout">
        <nav class="doc-sidebar">
            <div class="doc-section">
                <a href="{{.NavPrefix}}index.html"{{if eq .CurrentPath "index"}} class="active"{{end}}>Home</a>
            </div>
            {{.Navigation}}
        </nav>
        <main class="doc-content">
            {{.Content}}
        </main>
    </div>
</body>
</html>`

// PageData holds data for rendering a documentation page.
type PageData struct {
	Title       string
	Content     template.HTML
	Navigation  template.HTML
	CurrentPath string
	BasePath    string
	NavPrefix   string // relative prefix to reach doc root from current page (e.g., "../")
}

var md goldmark.Markdown

func init() {
	md = goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
		),
	)
}

// ConvertToHTML converts markdown content to a full HTML page with navigation.
func ConvertToHTML(mdContent []byte, nav []DocEntry, currentPath, basePath string) ([]byte, error) {
	// Transform .md links to .html
	mdContent = TransformMarkdownLinks(mdContent)

	// Convert markdown to HTML
	var htmlBuf bytes.Buffer
	if err := md.Convert(mdContent, &htmlBuf); err != nil {
		return nil, fmt.Errorf("converting markdown: %w", err)
	}

	// Get title
	title := GetTitle(mdContent, currentPath)

	// Render navigation
	navHTML := renderNavigation(nav, currentPath)

	// Parse and execute template
	tmpl, err := template.New("doc").Parse(docTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	data := PageData{
		Title:       title,
		Content:     template.HTML(htmlBuf.String()),
		Navigation:  navHTML,
		CurrentPath: currentPath,
		BasePath:    basePath,
		NavPrefix:   relativePrefix(currentPath),
	}

	var outBuf bytes.Buffer
	if err := tmpl.Execute(&outBuf, data); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}

	return outBuf.Bytes(), nil
}

// relativePrefix returns the "../" prefix needed to navigate from the
// current page's directory back to the documentation root.
func relativePrefix(currentPath string) string {
	depth := strings.Count(currentPath, "/")
	if depth == 0 {
		return ""
	}
	return strings.Repeat("../", depth)
}

// renderNavigation generates HTML for the navigation sidebar.
func renderNavigation(entries []DocEntry, currentPath string) template.HTML {
	var buf bytes.Buffer
	prefix := relativePrefix(currentPath)

	for _, entry := range entries {
		if entry.IsDir && len(entry.Children) > 0 {
			buf.WriteString(`<div class="doc-section">`)
			buf.WriteString(`<div class="doc-section-title">`)
			buf.WriteString(template.HTMLEscapeString(entry.Title))
			buf.WriteString(`</div>`)

			for _, child := range entry.Children {
				activeClass := ""
				if child.Path == currentPath {
					activeClass = ` class="active"`
				}
				fmt.Fprintf(&buf, `<a href="%s%s"%s>%s</a>`,
					prefix,
					template.HTMLEscapeString(child.HTMLPath),
					activeClass,
					template.HTMLEscapeString(child.Title),
				)
			}

			buf.WriteString(`</div>`)
		} else if !entry.IsDir {
			activeClass := ""
			if entry.Path == currentPath {
				activeClass = ` class="active"`
			}
			buf.WriteString(`<div class="doc-section">`)
			fmt.Fprintf(&buf, `<a href="%s%s"%s>%s</a>`,
				prefix,
				template.HTMLEscapeString(entry.HTMLPath),
				activeClass,
				template.HTMLEscapeString(entry.Title),
			)
			buf.WriteString(`</div>`)
		}
	}

	return template.HTML(buf.String())
}

// linkTransformRegex matches markdown links with .md extension.
var linkTransformRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\.md\)`)

// transformLinks converts .md links to .html in HTML content.
// This is used for any remaining raw links in the output.
func transformLinks(content string) string {
	return linkTransformRegex.ReplaceAllString(content, `<a href="$2.html">$1</a>`)
}

// NormalizePath normalizes a path for comparison.
func NormalizePath(path string) string {
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".html")
	path = strings.TrimSuffix(path, ".md")
	if path == "" || path == "index" {
		return "index"
	}
	return path
}

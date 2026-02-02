package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"

	"github.com/yuin/goldmark"
)

//go:embed layouts/*.html pages/*.html partials/*.html overlay/*.html
var templateFS embed.FS

type Engine struct {
	templates map[string]*template.Template
	overlay   *template.Template
}

func New() (*Engine, error) {
	engine := &Engine{
		templates: make(map[string]*template.Template),
	}

	md := goldmark.New()

	funcMap := template.FuncMap{
		"upper":    strings.ToUpper,
		"lower":    strings.ToLower,
		"contains": strings.Contains,
		"join":     strings.Join,
		"safe":     func(s string) template.HTML { return template.HTML(s) },
		"markdown": func(s string) template.HTML {
			var buf bytes.Buffer
			if err := md.Convert([]byte(s), &buf); err != nil {
				return template.HTML(template.HTMLEscapeString(s))
			}
			return template.HTML(buf.String())
		},
	}

	// Parse page templates, each extending the base layout
	pages, err := templateFS.ReadDir("pages")
	if err != nil {
		return nil, fmt.Errorf("reading pages directory: %w", err)
	}

	for _, page := range pages {
		if page.IsDir() {
			continue
		}
		name := page.Name()

		t, err := template.New("base.html").Funcs(funcMap).ParseFS(templateFS,
			"layouts/base.html",
			"partials/*.html",
			"pages/"+name,
		)
		if err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", name, err)
		}

		// Key by page name without extension
		key := strings.TrimSuffix(name, ".html")
		engine.templates[key] = t
	}

	// Parse the overlay template separately (not a full page template)
	overlayTmpl, err := template.New("overlay").Funcs(funcMap).ParseFS(templateFS, "overlay/doc_overlay.html")
	if err != nil {
		return nil, fmt.Errorf("parsing overlay template: %w", err)
	}
	engine.overlay = overlayTmpl

	return engine, nil
}

func (e *Engine) Render(w io.Writer, name string, data any) error {
	t, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}
	return t.Execute(w, data)
}

// OverlayData holds the data needed for the doc overlay.
type OverlayData struct {
	Slug        string
	ProjectName string
	Version     string
}

// RenderOverlay renders the doc overlay HTML snippet.
func (e *Engine) RenderOverlay(data OverlayData) (string, error) {
	var buf bytes.Buffer
	if err := e.overlay.ExecuteTemplate(&buf, "doc_overlay.html", data); err != nil {
		return "", fmt.Errorf("rendering overlay: %w", err)
	}
	return buf.String(), nil
}

package templates

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"
)

//go:embed layouts/*.html pages/*.html partials/*.html overlay/*.html
var templateFS embed.FS

type Engine struct {
	templates map[string]*template.Template
}

func New() (*Engine, error) {
	engine := &Engine{
		templates: make(map[string]*template.Template),
	}

	funcMap := template.FuncMap{
		"upper":    strings.ToUpper,
		"lower":    strings.ToLower,
		"contains": strings.Contains,
		"join":     strings.Join,
		"safe":     func(s string) template.HTML { return template.HTML(s) },
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

	return engine, nil
}

func (e *Engine) Render(w io.Writer, name string, data any) error {
	t, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}
	return t.Execute(w, data)
}

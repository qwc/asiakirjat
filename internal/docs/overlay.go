package docs

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

// InjectOverlay wraps an http.ResponseWriter to inject overlay HTML before </body>
// in HTML responses. Non-HTML responses are passed through unchanged.
func InjectOverlay(w http.ResponseWriter, r *http.Request, overlayHTML string, serve func(http.ResponseWriter, *http.Request)) {
	rec := &overlayRecorder{
		ResponseWriter: w,
		body:           &bytes.Buffer{},
	}

	serve(rec, r)

	contentType := rec.Header().Get("Content-Type")
	isHTML := strings.Contains(contentType, "text/html")

	if isHTML && rec.body.Len() > 0 {
		body := rec.body.String()
		injected := injectBeforeBodyClose(body, overlayHTML)
		w.Header().Set("Content-Length", "")
		w.Header().Del("Content-Length")
		for k, vs := range rec.Header() {
			if k == "Content-Length" {
				continue
			}
			for _, v := range vs {
				w.Header().Set(k, v)
			}
		}
		if rec.statusCode != 0 {
			w.WriteHeader(rec.statusCode)
		}
		io.WriteString(w, injected)
	} else {
		// Non-HTML: copy headers and body as-is
		for k, vs := range rec.Header() {
			for _, v := range vs {
				w.Header().Set(k, v)
			}
		}
		if rec.statusCode != 0 {
			w.WriteHeader(rec.statusCode)
		}
		w.Write(rec.body.Bytes())
	}
}

// injectBeforeBodyClose inserts the overlay HTML just before </body>.
// If no </body> tag is found, appends to the end.
func injectBeforeBodyClose(html, overlay string) string {
	lowerHTML := strings.ToLower(html)
	idx := strings.LastIndex(lowerHTML, "</body>")
	if idx == -1 {
		return html + overlay
	}
	return html[:idx] + overlay + html[idx:]
}

// overlayRecorder captures the response so we can inspect and modify it.
type overlayRecorder struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (r *overlayRecorder) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (r *overlayRecorder) WriteHeader(code int) {
	r.statusCode = code
}

package docs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInjectBeforeBodyClose(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		overlay  string
		expected string
	}{
		{
			name:     "basic injection",
			html:     "<html><body><h1>Hello</h1></body></html>",
			overlay:  "<div>overlay</div>",
			expected: "<html><body><h1>Hello</h1><div>overlay</div></body></html>",
		},
		{
			name:     "no body tag",
			html:     "<html><h1>Hello</h1></html>",
			overlay:  "<div>overlay</div>",
			expected: "<html><h1>Hello</h1></html><div>overlay</div>",
		},
		{
			name:     "case insensitive body",
			html:     "<html><body><p>text</p></BODY></html>",
			overlay:  "<div>bar</div>",
			expected: "<html><body><p>text</p><div>bar</div></BODY></html>",
		},
		{
			name:     "empty body",
			html:     "",
			overlay:  "<div>overlay</div>",
			expected: "<div>overlay</div>",
		},
		{
			name:     "multiple body tags uses last",
			html:     "<html><body></body><body></body></html>",
			overlay:  "<div>x</div>",
			expected: "<html><body></body><body><div>x</div></body></html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := injectBeforeBodyClose(tt.html, tt.overlay)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestInjectOverlay_HTMLResponse(t *testing.T) {
	overlay := `<div id="overlay">test</div>`

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><body><h1>Doc</h1></body></html>"))
	})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	InjectOverlay(rec, req, overlay, handler.ServeHTTP)

	body := rec.Body.String()
	if !strings.Contains(body, `<div id="overlay">test</div>`) {
		t.Error("expected overlay in HTML response")
	}
	if !strings.Contains(body, "<h1>Doc</h1>") {
		t.Error("expected original content preserved")
	}
	// Overlay should appear before </body>
	overlayIdx := strings.Index(body, `<div id="overlay">`)
	bodyCloseIdx := strings.Index(strings.ToLower(body), "</body>")
	if overlayIdx > bodyCloseIdx {
		t.Error("overlay should appear before </body>")
	}
}

func TestInjectOverlay_NonHTML(t *testing.T) {
	overlay := `<div id="overlay">test</div>`

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte("body { color: red; }"))
	})

	req := httptest.NewRequest("GET", "/style.css", nil)
	rec := httptest.NewRecorder()

	InjectOverlay(rec, req, overlay, handler.ServeHTTP)

	body := rec.Body.String()
	if strings.Contains(body, "overlay") {
		t.Error("overlay should NOT be injected into CSS response")
	}
	if body != "body { color: red; }" {
		t.Errorf("expected original CSS content, got %q", body)
	}
}

func TestInjectOverlay_ImageResponse(t *testing.T) {
	overlay := `<div id="overlay">test</div>`

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47}) // PNG magic bytes
	})

	req := httptest.NewRequest("GET", "/image.png", nil)
	rec := httptest.NewRecorder()

	InjectOverlay(rec, req, overlay, handler.ServeHTTP)

	body := rec.Body.Bytes()
	if len(body) != 4 {
		t.Errorf("expected 4 bytes, got %d", len(body))
	}
}

func TestInjectOverlay_PreservesStatusCode(t *testing.T) {
	overlay := `<div>overlay</div>`

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("<html><body>Not Found</body></html>"))
	})

	req := httptest.NewRequest("GET", "/missing.html", nil)
	rec := httptest.NewRecorder()

	InjectOverlay(rec, req, overlay, handler.ServeHTTP)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 status, got %d", rec.Code)
	}
}

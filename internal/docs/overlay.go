package docs

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

// InjectOverlay wraps an http.ResponseWriter to inject overlay HTML before
// </body> in HTML responses. Everything else is passed straight through.
//
// Only HTML is buffered. The recorder decides from the Content-Type as soon as
// the handler commits to a status, and from then on a non-HTML response is
// written directly to the client — it used to be collected in memory in full
// first, whatever it was, and this path also serves extension-less URLs, which
// can be any file at all (audit L-5).
func InjectOverlay(w http.ResponseWriter, r *http.Request, overlayHTML string, serve func(http.ResponseWriter, *http.Request)) {
	rec := &overlayRecorder{ResponseWriter: w}

	serve(rec, r)

	if rec.passthrough {
		// Headers and status are already on their way; the body went with
		// them. Nothing left to do beyond making sure a handler that wrote
		// nothing at all still gets its status out.
		rec.commit()
		return
	}

	// HTML: the whole body is in hand, so inject and send it. The recorded
	// length is no longer right.
	w.Header().Del("Content-Length")
	if rec.statusCode != 0 {
		w.WriteHeader(rec.statusCode)
	}
	io.WriteString(w, injectBeforeBodyClose(rec.body.String(), overlayHTML))
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

// overlayRecorder buffers an HTML response so the overlay can be inserted into
// it, and gets out of the way for anything else.
//
// Header() is the embedded writer's own map, so headers set by the handler are
// already where they need to be — only the body and the status are held back.
type overlayRecorder struct {
	http.ResponseWriter
	body        bytes.Buffer
	statusCode  int
	decided     bool
	passthrough bool
	committed   bool
}

// decide fixes whether this response is injected into or streamed, using the
// Content-Type the handler has set by the time it writes anything.
//
// A response that is not a plain 200 is streamed even when it is HTML: a 206
// carries part of a file, and inserting an overlay into a byte range would
// corrupt it.
func (r *overlayRecorder) decide() {
	if r.decided {
		return
	}
	r.decided = true

	status := r.statusCode
	if status == 0 {
		status = http.StatusOK
	}
	isHTML := strings.Contains(r.Header().Get("Content-Type"), "text/html")
	r.passthrough = !isHTML || status != http.StatusOK

	if r.passthrough {
		r.commit()
	}
}

// commit sends the status line once, for the streaming case.
func (r *overlayRecorder) commit() {
	if r.committed {
		return
	}
	r.committed = true
	if r.statusCode != 0 {
		r.ResponseWriter.WriteHeader(r.statusCode)
	}
}

func (r *overlayRecorder) Write(b []byte) (int, error) {
	r.decide()
	if r.passthrough {
		return r.ResponseWriter.Write(b)
	}
	return r.body.Write(b)
}

func (r *overlayRecorder) WriteHeader(code int) {
	if r.decided {
		return
	}
	r.statusCode = code
	r.decide()
}

// Flush passes through once the response is streaming. While a response is
// still being buffered for injection there is nothing to flush: the overlay
// has to go in before any of it reaches the client.
func (r *overlayRecorder) Flush() {
	if !r.passthrough {
		return
	}
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

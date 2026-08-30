package handler

import (
	"net/http"
)

// userError reports a failure to the user and keeps the cause in the log.
//
// The message is written verbatim, so it must be something safe to show:
// database errors carry table and column names and driver text, and
// filesystem errors carry absolute storage paths. Handlers used to pass
// err.Error() straight into http.Error, which put all of that on screen
// (audit L-4).
//
// logKV are extra slog key/value pairs for the log line — the slug or user id
// that gives the entry context.
func (h *Handler) userError(w http.ResponseWriter, status int, message string, err error, logKV ...any) {
	args := append([]any{"error", err, "status", status}, logKV...)
	h.logger.Error(message, args...)
	http.Error(w, message, status)
}

// jsonUserError is userError for the API: same split between what is shown
// and what is logged, in the shape API clients expect.
func (h *Handler) jsonUserError(w http.ResponseWriter, status int, message string, err error, logKV ...any) {
	args := append([]any{"error", err, "status", status}, logKV...)
	h.logger.Error(message, args...)
	h.jsonError(w, message, status)
}

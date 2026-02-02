package handler

import (
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
)

func (h *Handler) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	h.render(w, "login", map[string]any{
		"User":          nil,
		"OAuth2Enabled": h.config.Auth.OAuth2.Enabled,
	})
}

func (h *Handler) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.render(w, "login", map[string]any{
			"Error":         "Username and password are required",
			"OAuth2Enabled": h.config.Auth.OAuth2.Enabled,
		})
		return
	}

	for _, a := range h.authenticators {
		user, err := a.Authenticate(r.Context(), username, password)
		if err == nil && user != nil {
			if err := h.sessionMgr.CreateSession(r.Context(), w, user.ID); err != nil {
				h.logger.Error("creating session", "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	h.render(w, "login", map[string]any{
		"Error":         "Invalid username or password",
		"OAuth2Enabled": h.config.Auth.OAuth2.Enabled,
	})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.sessionMgr.DestroySession(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) handleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	// OAuth2 callback — implemented in Phase 9
	http.Error(w, "OAuth2 not configured", http.StatusNotImplemented)
}

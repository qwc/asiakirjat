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

func (h *Handler) handleOAuth2Login(w http.ResponseWriter, r *http.Request) {
	if h.oauth2Auth == nil {
		http.Error(w, "OAuth2 not configured", http.StatusNotImplemented)
		return
	}

	authURL, err := h.oauth2Auth.GenerateAuthURL()
	if err != nil {
		h.logger.Error("generating OAuth2 auth URL", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.sessionMgr.DestroySession(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) handleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	if h.oauth2Auth == nil {
		http.Error(w, "OAuth2 not configured", http.StatusNotImplemented)
		return
	}

	// Validate CSRF state
	state := r.URL.Query().Get("state")
	if !h.oauth2Auth.ValidateState(state) {
		h.render(w, "login", map[string]any{
			"Error":         "Invalid OAuth2 state (CSRF check failed)",
			"OAuth2Enabled": true,
		})
		return
	}

	// Exchange code for user
	code := r.URL.Query().Get("code")
	if code == "" {
		h.render(w, "login", map[string]any{
			"Error":         "Missing authorization code",
			"OAuth2Enabled": true,
		})
		return
	}

	user, err := h.oauth2Auth.HandleCallback(r.Context(), code)
	if err != nil {
		h.logger.Error("OAuth2 callback failed", "error", err)
		h.render(w, "login", map[string]any{
			"Error":         "OAuth2 authentication failed",
			"OAuth2Enabled": true,
		})
		return
	}

	if err := h.sessionMgr.CreateSession(r.Context(), w, user.ID); err != nil {
		h.logger.Error("creating session after OAuth2", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

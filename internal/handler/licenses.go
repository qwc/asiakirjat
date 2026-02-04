package handler

import (
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/licenses"
)

func (h *Handler) handleLicenses(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	h.render(w, "licenses", map[string]any{
		"User": user,
		"Deps": licenses.Deps,
	})
}

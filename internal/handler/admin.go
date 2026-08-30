package handler

import (
	"net/http"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/docs/builtin"
)

func (h *Handler) handleAdminDeployBuiltinDocs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	deployer := &builtin.Deployer{
		Storage:     h.storage,
		Projects:    h.projects,
		Versions:    h.versions,
		SearchIndex: h.searchIndex,
		BasePath:    h.config.Server.BasePath,
		Logger:      h.logger,
	}

	if err := deployer.Deploy(ctx, user.ID); err != nil {
		h.userError(w, http.StatusInternalServerError,
			"Failed to deploy the built-in documentation — see the server log for the cause",
			err)
		return
	}

	h.invalidateLatestTagsCache()
	h.redirect(w, r, "/admin/projects?msg=docs_deployed", http.StatusSeeOther)
}

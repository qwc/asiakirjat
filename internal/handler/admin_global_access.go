package handler

import (
	"net/http"
	"strconv"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Admin handlers for the global access list that governs private projects.

func (h *Handler) handleAdminGlobalAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	rules, err := h.globalAccess.ListRules(ctx)
	if err != nil {
		h.logger.Error("listing global access rules", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"User":  user,
		"Rules": rules,
	}

	switch r.URL.Query().Get("msg") {
	case "created":
		data["Flash"] = &Flash{
			Type:    "success",
			Message: "Global access rule created",
		}
	case "deleted":
		data["Flash"] = &Flash{
			Type:    "success",
			Message: "Global access rule deleted",
		}
	case "error":
		data["Flash"] = &Flash{
			Type:    "error",
			Message: r.URL.Query().Get("error"),
		}
	}

	h.render(w, r, "admin_global_access", data)
}

func (h *Handler) handleAdminCreateGlobalAccessRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	subjectType := r.FormValue("subject_type")
	subjectIdentifier := r.FormValue("subject_identifier")
	role := r.FormValue("role")

	if subjectType != "user" && subjectType != "ldap_group" && subjectType != "oauth2_group" {
		h.redirect(w, r, "/admin/global-access?msg=error&error=Invalid+subject+type", http.StatusSeeOther)
		return
	}

	if subjectIdentifier == "" {
		h.redirect(w, r, "/admin/global-access?msg=error&error=Identifier+is+required", http.StatusSeeOther)
		return
	}

	if role != "viewer" && role != "editor" {
		role = "viewer"
	}

	rule := &database.GlobalAccess{
		SubjectType:       subjectType,
		SubjectIdentifier: subjectIdentifier,
		Role:              role,
		FromConfig:        false,
	}

	if err := h.globalAccess.CreateRule(ctx, rule); err != nil {
		h.logger.Error("creating global access rule", "error", err)
		h.redirect(w, r, "/admin/global-access?msg=error&error=Failed+to+create+rule", http.StatusSeeOther)
		return
	}

	h.redirect(w, r, "/admin/global-access?msg=created", http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteGlobalAccessRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid rule ID", http.StatusBadRequest)
		return
	}

	if err := h.globalAccess.DeleteRule(ctx, id); err != nil {
		h.logger.Error("deleting global access rule", "error", err)
		h.redirect(w, r, "/admin/global-access?msg=error&error=Failed+to+delete+rule", http.StatusSeeOther)
		return
	}

	h.redirect(w, r, "/admin/global-access?msg=deleted", http.StatusSeeOther)
}

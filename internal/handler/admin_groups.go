package handler

import (
	"net/http"
	"strconv"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Admin handlers for LDAP/OAuth2 group mappings.

func (h *Handler) handleAdminGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	// Get all group mappings
	mappings, err := h.groupMappings.List(ctx)
	if err != nil {
		h.logger.Error("listing group mappings", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Get all projects for the dropdown and for mapping display
	projects, err := h.projects.List(ctx)
	if err != nil {
		h.logger.Error("listing projects", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Build project name lookup
	projectNames := make(map[int64]string)
	for _, p := range projects {
		projectNames[p.ID] = p.Name
	}

	// Build grouped view models
	// Key: "authSource|groupIdentifier|role"
	groupedMap := make(map[string]*groupMappingGrouped)
	var groupOrder []string // preserve order

	for _, m := range mappings {
		key := m.AuthSource + "|" + m.GroupIdentifier + "|" + m.Role
		if _, exists := groupedMap[key]; !exists {
			groupedMap[key] = &groupMappingGrouped{
				AuthSource:      m.AuthSource,
				GroupIdentifier: m.GroupIdentifier,
				Role:            m.Role,
				Projects:        []groupMappingProject{},
			}
			groupOrder = append(groupOrder, key)
		}
		groupedMap[key].Projects = append(groupedMap[key].Projects, groupMappingProject{
			MappingID:   m.ID,
			ProjectID:   m.ProjectID,
			ProjectName: projectNames[m.ProjectID],
			FromConfig:  m.FromConfig,
		})
	}

	// Convert to slice preserving order
	var grouped []groupMappingGrouped
	for _, key := range groupOrder {
		grouped = append(grouped, *groupedMap[key])
	}

	data := map[string]any{
		"User":     user,
		"Mappings": grouped,
		"Projects": projects,
	}

	// Check for flash message from query parameter
	switch r.URL.Query().Get("msg") {
	case "created":
		data["Flash"] = &Flash{
			Type:    "success",
			Message: "Group mapping created successfully",
		}
	case "deleted":
		data["Flash"] = &Flash{
			Type:    "success",
			Message: "Group mapping deleted successfully",
		}
	case "error":
		data["Flash"] = &Flash{
			Type:    "error",
			Message: r.URL.Query().Get("error"),
		}
	}

	h.render(w, r, "admin_groups", data)
}

func (h *Handler) handleAdminCreateGroupMapping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	authSource := r.FormValue("auth_source")
	groupIdentifier := r.FormValue("group_identifier")
	projectIDs := r.Form["project_ids[]"] // Multiple project IDs
	role := r.FormValue("role")

	if authSource != "ldap" && authSource != "oauth2" {
		h.redirect(w, r, "/admin/groups?msg=error&error=Invalid+auth+source", http.StatusSeeOther)
		return
	}

	if groupIdentifier == "" {
		h.redirect(w, r, "/admin/groups?msg=error&error=Group+identifier+required", http.StatusSeeOther)
		return
	}

	if len(projectIDs) == 0 {
		h.redirect(w, r, "/admin/groups?msg=error&error=At+least+one+project+required", http.StatusSeeOther)
		return
	}

	if role != "viewer" && role != "editor" {
		role = "viewer"
	}

	// Create a mapping for each project
	var created int
	for _, pidStr := range projectIDs {
		projectID, err := strconv.ParseInt(pidStr, 10, 64)
		if err != nil {
			continue
		}

		mapping := &database.AuthGroupMapping{
			AuthSource:      authSource,
			GroupIdentifier: groupIdentifier,
			ProjectID:       projectID,
			Role:            role,
			FromConfig:      false,
		}

		if err := h.groupMappings.Create(ctx, mapping); err != nil {
			// Log but continue - might be duplicate
			h.logger.Warn("creating group mapping", "error", err, "project_id", projectID)
			continue
		}
		created++
	}

	if created == 0 {
		h.redirect(w, r, "/admin/groups?msg=error&error=Failed+to+create+mappings", http.StatusSeeOther)
		return
	}

	h.redirect(w, r, "/admin/groups?msg=created", http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteGroupMapping(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid mapping ID", http.StatusBadRequest)
		return
	}

	// Check if mapping exists and is not from config
	mapping, err := h.groupMappings.GetByID(ctx, id)
	if err != nil {
		http.Error(w, "Mapping not found", http.StatusNotFound)
		return
	}

	if mapping.FromConfig {
		h.redirect(w, r, "/admin/groups?msg=error&error=Cannot+delete+config-sourced+mappings", http.StatusSeeOther)
		return
	}

	if err := h.groupMappings.Delete(ctx, id); err != nil {
		h.logger.Error("deleting group mapping", "error", err)
		h.redirect(w, r, "/admin/groups?msg=error&error=Failed+to+delete+mapping", http.StatusSeeOther)
		return
	}

	// Revoke project_access rows for this project granted by the same
	// auth source. Surviving mappings re-grant on next login; this keeps
	// the dangling-grant window short instead of indefinite (audit M-1).
	if err := h.access.RevokeProjectBySource(ctx, mapping.ProjectID, mapping.AuthSource); err != nil {
		h.logger.Error("revoking project access after group mapping delete", "project_id", mapping.ProjectID, "source", mapping.AuthSource, "error", err)
	}

	h.redirect(w, r, "/admin/groups?msg=deleted", http.StatusSeeOther)
}

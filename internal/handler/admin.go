package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs/builtin"
)

func (h *Handler) handleAdminProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	isAdmin := user != nil && user.Role == "admin"

	allProjects, err := h.projects.List(ctx)
	if err != nil {
		h.logger.Error("listing projects", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Admins see all projects; editors only see projects they have access to
	var projects []database.Project
	if isAdmin {
		projects = allProjects
	} else {
		projects = h.filterAccessibleProjects(ctx, user, allProjects)
	}

	data := map[string]any{
		"User":            user,
		"IsAdmin":         isAdmin,
		"Projects":        projects,
		"ReindexRunning":  h.reindexRunning,
		"ReindexProgress": h.reindexProgress,
	}

	// Check for flash message from query parameter
	switch r.URL.Query().Get("msg") {
	case "reindex_started":
		data["Flash"] = &Flash{
			Type:    "success",
			Message: "Search index rebuild started in background",
		}
	case "reindex_already_running":
		data["Flash"] = &Flash{
			Type:    "warning",
			Message: "Reindex is already running",
		}
	case "docs_deployed":
		data["Flash"] = &Flash{
			Type:    "success",
			Message: "Built-in documentation deployed successfully",
		}
	}

	h.render(w, "admin_projects", data)
}

func (h *Handler) handleAdminCreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slug := r.FormValue("slug")
	name := r.FormValue("name")
	description := r.FormValue("description")
	visibility := r.FormValue("visibility")
	if visibility != database.VisibilityPublic && visibility != database.VisibilityPrivate && visibility != database.VisibilityCustom {
		visibility = database.VisibilityCustom
	}

	// Parse retention_days
	var retentionDays *int
	if rd := r.FormValue("retention_days"); rd != "" {
		if days, err := strconv.Atoi(rd); err == nil && days >= 0 {
			retentionDays = &days
		}
	}

	project := &database.Project{
		Slug:          slug,
		Name:          name,
		Description:   description,
		Visibility:    visibility,
		RetentionDays: retentionDays,
	}

	if err := h.projects.Create(ctx, project); err != nil {
		h.logger.Error("creating project", "error", err)
		http.Error(w, "Failed to create project: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.storage.EnsureProjectDir(slug); err != nil {
		h.logger.Error("creating project directory", "error", err)
	}

	// Auto-grant editor access to the creator for non-public projects
	creator := auth.UserFromContext(ctx)
	if creator != nil && creator.Role != "admin" && visibility != database.VisibilityPublic {
		access := &database.ProjectAccess{
			ProjectID: project.ID,
			UserID:    creator.ID,
			Role:      "editor",
		}
		if err := h.access.Grant(ctx, access); err != nil {
			h.logger.Error("auto-granting creator access", "error", err)
		}
	}

	h.redirect(w, r, "/admin/projects", http.StatusSeeOther)
}

func (h *Handler) handleAdminEditProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	accessList, _ := h.access.ListByProject(ctx, project.ID)
	users, _ := h.users.List(ctx)

	type accessView struct {
		UserID   int64
		Username string
		Role     string
	}
	var accessViews []accessView
	userMap := make(map[int64]string)
	for _, u := range users {
		userMap[u.ID] = u.Username
	}
	for _, a := range accessList {
		accessViews = append(accessViews, accessView{
			UserID:   a.UserID,
			Username: userMap[a.UserID],
			Role:     a.Role,
		})
	}

	// Build retention display info
	globalDefault := h.config.Retention.NonSemverDays
	retentionDisplay := ""
	if project.RetentionDays != nil {
		retentionDisplay = strconv.Itoa(*project.RetentionDays)
	}
	globalRetentionLabel := "unlimited"
	if globalDefault > 0 {
		globalRetentionLabel = strconv.Itoa(globalDefault) + " days"
	}

	h.render(w, "admin_project_edit", map[string]any{
		"User":                  user,
		"Project":               project,
		"AccessList":            accessViews,
		"Users":                 users,
		"RetentionDisplay":      retentionDisplay,
		"GlobalRetentionDefault": globalRetentionLabel,
	})
}

func (h *Handler) handleAdminUpdateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	project.Slug = r.FormValue("slug")
	project.Name = r.FormValue("name")
	project.Description = r.FormValue("description")
	visibility := r.FormValue("visibility")
	if visibility != database.VisibilityPublic && visibility != database.VisibilityPrivate && visibility != database.VisibilityCustom {
		visibility = database.VisibilityCustom
	}
	project.Visibility = visibility

	// Parse retention_days: empty = NULL (use global default), "0" = unlimited, positive = override
	if rd := r.FormValue("retention_days"); rd == "" {
		project.RetentionDays = nil
	} else if days, err := strconv.Atoi(rd); err == nil && days >= 0 {
		project.RetentionDays = &days
	} else {
		project.RetentionDays = nil
	}

	if err := h.projects.Update(ctx, project); err != nil {
		h.logger.Error("updating project", "error", err)
		http.Error(w, "Failed to update project", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/admin/projects", http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Delete search index entries for all versions before deleting project
	if h.searchIndex != nil {
		versions, err := h.versions.ListByProject(ctx, project.ID)
		if err == nil {
			for _, v := range versions {
				if err := h.searchIndex.DeleteVersion(project.ID, v.ID); err != nil {
					h.logger.Error("deleting version from search index", "error", err, "project", slug, "version", v.Tag)
				}
			}
		}
	}

	if err := h.projects.Delete(ctx, project.ID); err != nil {
		h.logger.Error("deleting project", "error", err)
		http.Error(w, "Failed to delete project", http.StatusInternalServerError)
		return
	}

	// Invalidate latest tags cache
	h.invalidateLatestTagsCache()

	h.redirect(w, r, "/admin/projects", http.StatusSeeOther)
}

func (h *Handler) handleAdminGrantAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	userID, err := strconv.ParseInt(r.FormValue("grant_user_id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	role := r.FormValue("grant_role")
	if role != "viewer" && role != "editor" {
		role = "viewer"
	}

	access := &database.ProjectAccess{
		ProjectID: project.ID,
		UserID:    userID,
		Role:      role,
	}

	if err := h.access.Grant(ctx, access); err != nil {
		h.logger.Error("granting access", "error", err)
		http.Error(w, "Failed to grant access", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, fmt.Sprintf("/admin/projects/%s/edit", slug), http.StatusSeeOther)
}

func (h *Handler) handleAdminRevokeAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	userID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	if err := h.access.Revoke(ctx, project.ID, userID); err != nil {
		h.logger.Error("revoking access", "error", err)
		http.Error(w, "Failed to revoke access", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, fmt.Sprintf("/admin/projects/%s/edit", slug), http.StatusSeeOther)
}

func (h *Handler) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	users, err := h.users.List(ctx)
	if err != nil {
		h.logger.Error("listing users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, "admin_users", map[string]any{
		"User":  user,
		"Users": users,
	})
}

func (h *Handler) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	username := r.FormValue("username")
	password := r.FormValue("password")
	email := r.FormValue("email")
	role := r.FormValue("role")

	if username == "" || password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	if role != "admin" && role != "editor" && role != "viewer" {
		role = "viewer"
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		h.logger.Error("hashing password", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	user := &database.User{
		Username:   username,
		Email:      email,
		Password:   &hash,
		AuthSource: "builtin",
		Role:       role,
	}

	if err := h.users.Create(ctx, user); err != nil {
		h.logger.Error("creating user", "error", err)
		http.Error(w, "Failed to create user: "+err.Error(), http.StatusBadRequest)
		return
	}

	h.redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	if err := h.users.Delete(ctx, id); err != nil {
		h.logger.Error("deleting user", "error", err)
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) handleAdminUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	role := r.FormValue("role")
	if role != "admin" && role != "editor" && role != "viewer" {
		http.Error(w, "Invalid role", http.StatusBadRequest)
		return
	}

	user, err := h.users.GetByID(ctx, id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	user.Role = role
	if err := h.users.Update(ctx, user); err != nil {
		h.logger.Error("updating user role", "error", err)
		http.Error(w, "Failed to update role", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) handleAdminRobots(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	robots, err := h.users.ListRobots(ctx)
	if err != nil {
		h.logger.Error("listing robots", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	projects, err := h.projects.List(ctx)
	if err != nil {
		h.logger.Error("listing projects", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Build project name lookup for token display
	projectNames := make(map[int64]string)
	for _, p := range projects {
		projectNames[p.ID] = p.Name
	}

	type tokenView struct {
		database.APIToken
		ProjectName string
	}

	type robotView struct {
		User    database.User
		Tokens  []tokenView
		RobotID int64
	}

	var robotViews []robotView
	for _, robot := range robots {
		tokens, _ := h.tokens.ListByUser(ctx, robot.ID)
		var tokenViews []tokenView
		for _, t := range tokens {
			tv := tokenView{APIToken: t}
			if t.ProjectID != nil {
				tv.ProjectName = projectNames[*t.ProjectID]
			}
			tokenViews = append(tokenViews, tv)
		}
		robotViews = append(robotViews, robotView{
			User:    robot,
			Tokens:  tokenViews,
			RobotID: robot.ID,
		})
	}

	h.render(w, "admin_robots", map[string]any{
		"User":     user,
		"Robots":   robotViews,
		"Projects": projects,
	})
}

func (h *Handler) handleAdminCreateRobot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	username := r.FormValue("username")
	if username == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		return
	}

	user := &database.User{
		Username:   username,
		AuthSource: "robot",
		Role:       "editor",
		IsRobot:    true,
	}

	if err := h.users.Create(ctx, user); err != nil {
		h.logger.Error("creating robot", "error", err)
		http.Error(w, "Failed to create robot: "+err.Error(), http.StatusBadRequest)
		return
	}

	h.redirect(w, r, "/admin/robots", http.StatusSeeOther)
}

func (h *Handler) handleAdminGenerateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	robotID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid robot ID", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		name = "default"
	}

	// Parse optional project_id for scoped tokens
	var projectID *int64
	if pidStr := r.FormValue("project_id"); pidStr != "" {
		pid, err := strconv.ParseInt(pidStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid project ID", http.StatusBadRequest)
			return
		}
		projectID = &pid
	}

	// Generate raw token
	rawToken, err := auth.GenerateToken(32)
	if err != nil {
		h.logger.Error("generating token", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	tokenHash := auth.HashToken(rawToken)

	token := &database.APIToken{
		UserID:    robotID,
		ProjectID: projectID,
		TokenHash: tokenHash,
		Name:      name,
		Scopes:    "upload",
	}

	if err := h.tokens.Create(ctx, token); err != nil {
		h.logger.Error("creating token", "error", err)
		http.Error(w, "Failed to create token", http.StatusInternalServerError)
		return
	}

	// Re-render robots page with the new token shown
	robots, _ := h.users.ListRobots(ctx)
	projects, _ := h.projects.List(ctx)

	// Build project name lookup for token display
	projectNames := make(map[int64]string)
	for _, p := range projects {
		projectNames[p.ID] = p.Name
	}

	type tokenView struct {
		database.APIToken
		ProjectName string
	}

	type robotView struct {
		User    database.User
		Tokens  []tokenView
		RobotID int64
	}

	var robotViews []robotView
	for _, robot := range robots {
		tokens, _ := h.tokens.ListByUser(ctx, robot.ID)
		var tokenViews []tokenView
		for _, t := range tokens {
			tv := tokenView{APIToken: t}
			if t.ProjectID != nil {
				tv.ProjectName = projectNames[*t.ProjectID]
			}
			tokenViews = append(tokenViews, tv)
		}
		robotViews = append(robotViews, robotView{
			User:    robot,
			Tokens:  tokenViews,
			RobotID: robot.ID,
		})
	}

	h.render(w, "admin_robots", map[string]any{
		"User":     user,
		"Robots":   robotViews,
		"Projects": projects,
		"NewToken": rawToken,
	})
}

func (h *Handler) handleAdminRevokeToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tokenID, err := strconv.ParseInt(r.PathValue("tid"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	if err := h.tokens.Delete(ctx, tokenID); err != nil {
		h.logger.Error("revoking token", "error", err)
		http.Error(w, "Failed to revoke token", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/admin/robots", http.StatusSeeOther)
}

func (h *Handler) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	newPassword := r.FormValue("password")
	if newPassword == "" {
		http.Error(w, "Password is required", http.StatusBadRequest)
		return
	}

	user, err := h.users.GetByID(ctx, id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	if user.AuthSource != "builtin" {
		http.Error(w, "Cannot reset password for non-builtin user", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		h.logger.Error("hashing password", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	user.Password = &hash
	if err := h.users.Update(ctx, user); err != nil {
		h.logger.Error("updating user password", "error", err)
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteRobot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid robot ID", http.StatusBadRequest)
		return
	}

	if err := h.users.Delete(ctx, id); err != nil {
		h.logger.Error("deleting robot", "error", err)
		http.Error(w, "Failed to delete robot", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/admin/robots", http.StatusSeeOther)
}

// Group mappings view struct for template - individual mapping
type groupMappingView struct {
	ID              int64
	AuthSource      string
	GroupIdentifier string
	ProjectID       int64
	ProjectName     string
	Role            string
	FromConfig      bool
}

// Grouped view for display - one group can have multiple projects
type groupMappingGrouped struct {
	AuthSource      string
	GroupIdentifier string
	Role            string
	Projects        []groupMappingProject
}

type groupMappingProject struct {
	MappingID   int64
	ProjectID   int64
	ProjectName string
	FromConfig  bool
}

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

	h.render(w, "admin_groups", data)
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

	h.redirect(w, r, "/admin/groups?msg=deleted", http.StatusSeeOther)
}

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

	h.render(w, "admin_global_access", data)
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
		h.logger.Error("deploying builtin docs", "error", err)
		http.Error(w, "Failed to deploy documentation: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.invalidateLatestTagsCache()
	h.redirect(w, r, "/admin/projects?msg=docs_deployed", http.StatusSeeOther)
}

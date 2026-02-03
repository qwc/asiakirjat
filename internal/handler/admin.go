package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

func (h *Handler) handleAdminProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	projects, err := h.projects.List(ctx)
	if err != nil {
		h.logger.Error("listing projects", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := map[string]any{
		"User":     user,
		"Projects": projects,
	}

	// Check for flash message from query parameter
	if msg := r.URL.Query().Get("msg"); msg == "reindex_started" {
		data["Flash"] = &Flash{
			Type:    "success",
			Message: "Search index rebuild started in background",
		}
	}

	h.render(w, "admin_projects", data)
}

func (h *Handler) handleAdminCreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slug := r.FormValue("slug")
	name := r.FormValue("name")
	description := r.FormValue("description")
	isPublic := r.FormValue("is_public") == "1"

	project := &database.Project{
		Slug:        slug,
		Name:        name,
		Description: description,
		IsPublic:    isPublic,
	}

	if err := h.projects.Create(ctx, project); err != nil {
		h.logger.Error("creating project", "error", err)
		http.Error(w, "Failed to create project: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.storage.EnsureProjectDir(slug); err != nil {
		h.logger.Error("creating project directory", "error", err)
	}

	http.Redirect(w, r, "/admin/projects", http.StatusSeeOther)
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

	h.render(w, "admin_project_edit", map[string]any{
		"User":       user,
		"Project":    project,
		"AccessList": accessViews,
		"Users":      users,
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
	project.IsPublic = r.FormValue("is_public") == "1"

	if err := h.projects.Update(ctx, project); err != nil {
		h.logger.Error("updating project", "error", err)
		http.Error(w, "Failed to update project", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/projects", http.StatusSeeOther)
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

	http.Redirect(w, r, "/admin/projects", http.StatusSeeOther)
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

	http.Redirect(w, r, fmt.Sprintf("/admin/projects/%s/edit", slug), http.StatusSeeOther)
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

	http.Redirect(w, r, fmt.Sprintf("/admin/projects/%s/edit", slug), http.StatusSeeOther)
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

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
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

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
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

	type robotView struct {
		User    database.User
		Tokens  []database.APIToken
		RobotID int64
	}

	var robotViews []robotView
	for _, robot := range robots {
		tokens, _ := h.tokens.ListByUser(ctx, robot.ID)
		robotViews = append(robotViews, robotView{
			User:    robot,
			Tokens:  tokens,
			RobotID: robot.ID,
		})
	}

	h.render(w, "admin_robots", map[string]any{
		"User":   user,
		"Robots": robotViews,
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

	http.Redirect(w, r, "/admin/robots", http.StatusSeeOther)
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

	type robotView struct {
		User    database.User
		Tokens  []database.APIToken
		RobotID int64
	}

	var robotViews []robotView
	for _, robot := range robots {
		tokens, _ := h.tokens.ListByUser(ctx, robot.ID)
		robotViews = append(robotViews, robotView{
			User:    robot,
			Tokens:  tokens,
			RobotID: robot.ID,
		})
	}

	h.render(w, "admin_robots", map[string]any{
		"User":     user,
		"Robots":   robotViews,
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

	http.Redirect(w, r, "/admin/robots", http.StatusSeeOther)
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

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
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

	http.Redirect(w, r, "/admin/robots", http.StatusSeeOther)
}

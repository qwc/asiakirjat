package handler

import (
	"net/http"
	"strconv"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Admin handlers for robot users and their API tokens.

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

	h.render(w, r, "admin_robots", map[string]any{
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
		h.userError(w, http.StatusBadRequest,
			"Failed to create robot user — the name may already be taken",
			err, "username", username)
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

	// The id comes from the URL, and a token authenticates as whichever user
	// it names — so minting one for a human account would hand out that
	// person's access as a bearer credential (#155).
	robot, err := h.users.GetByID(ctx, robotID)
	if err != nil {
		http.Error(w, "Robot not found", http.StatusNotFound)
		return
	}
	if !robot.IsRobot {
		http.Error(w, "Tokens can only be issued to robot users", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		name = "default"
	}

	// Parse optional project_id for scoped tokens. A token scoped to a project
	// that does not exist can never authenticate anything, so it is a typo
	// worth catching here rather than a 401 in someone's CI next week.
	var projectID *int64
	if pidStr := r.FormValue("project_id"); pidStr != "" {
		pid, err := strconv.ParseInt(pidStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid project ID", http.StatusBadRequest)
			return
		}
		if _, err := h.projects.GetByID(ctx, pid); err != nil {
			http.Error(w, "Project not found", http.StatusBadRequest)
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

	h.render(w, r, "admin_robots", map[string]any{
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

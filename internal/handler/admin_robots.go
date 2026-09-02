package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
)

// Admin handlers for robot users and their API tokens.

// robotGrantView is one row of "what this robot can reach": the scope it was
// granted on, named, and the role it holds there.
type robotGrantView struct {
	ID    int64
	Scope string // "organization" or "project"
	Name  string
	Role  string
}

type robotTokenView struct {
	database.APIToken
	ProjectName string
	MayCreate   bool
}

type robotView struct {
	User    database.User
	Tokens  []robotTokenView
	Grants  []robotGrantView
	RobotID int64
}

// renderRobots draws the page. Both the list and the two forms that change it
// end here, so a new column is added once rather than in every handler that
// happens to re-render.
func (h *Handler) renderRobots(w http.ResponseWriter, r *http.Request, newToken string) {
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
	projectNames := make(map[int64]string, len(projects))
	for _, p := range projects {
		projectNames[p.ID] = p.Name
	}

	orgs, err := h.orgs.List(ctx)
	if err != nil {
		h.logger.Error("listing organizations", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	orgNames := make(map[int64]string, len(orgs))
	for _, o := range orgs {
		orgNames[o.ID] = o.Name
	}

	robotViews := make([]robotView, 0, len(robots))
	for _, robot := range robots {
		tokens, _ := h.tokens.ListByUser(ctx, robot.ID)
		tokenViews := make([]robotTokenView, 0, len(tokens))
		for _, t := range tokens {
			tv := robotTokenView{APIToken: t, MayCreate: tokenAllows(&t, scopeCreate)}
			if t.ProjectID != nil {
				tv.ProjectName = projectNames[*t.ProjectID]
			}
			tokenViews = append(tokenViews, tv)
		}

		grants, _ := h.accessGrants.ListByUser(ctx, robot.ID)
		grantViews := make([]robotGrantView, 0, len(grants))
		for _, g := range grants {
			switch {
			case g.OrgID != nil:
				grantViews = append(grantViews, robotGrantView{
					ID: g.ID, Scope: "organization", Name: orgNames[*g.OrgID], Role: g.Role,
				})
			case g.ProjectID != nil:
				grantViews = append(grantViews, robotGrantView{
					ID: g.ID, Scope: "project", Name: projectNames[*g.ProjectID], Role: g.Role,
				})
			}
		}

		robotViews = append(robotViews, robotView{
			User: robot, Tokens: tokenViews, Grants: grantViews, RobotID: robot.ID,
		})
	}

	data := map[string]any{
		"User":     user,
		"Robots":   robotViews,
		"Projects": projects,
		"Orgs":     orgs,
	}
	if newToken != "" {
		data["NewToken"] = newToken
	}
	applyFlash(data, r, map[string]string{
		"granted": "Access granted.",
		"revoked": "Access revoked.",
	})
	h.render(w, r, "admin_robots", data)
}

func (h *Handler) handleAdminRobots(w http.ResponseWriter, r *http.Request) {
	h.renderRobots(w, r, "")
}

// handleAdminCreateRobot creates a robot and, optionally, the grant that gives
// it somewhere to upload.
//
// A robot is an ordinary subject now: it holds the viewer role like any other
// account and reaches what it has been granted (#155). It used to be created
// as an instance editor, which meant every robot could upload to every project
// and only a token's project_id ever narrowed it.
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
		Role:       "viewer",
		IsRobot:    true,
	}

	if err := h.users.Create(ctx, user); err != nil {
		h.userError(w, http.StatusBadRequest,
			"Failed to create robot user — the name may already be taken",
			err, "username", username)
		return
	}

	// The scope picker is optional: a robot with no grant yet is a valid
	// thing to have, it just cannot upload until it is given somewhere to.
	if scope := r.FormValue("scope"); scope != "" {
		if problem := h.grantRobotScope(ctx, user.ID, scope, r.FormValue("role")); problem != "" {
			h.robotError(w, r, problem)
			return
		}
	}

	h.redirect(w, r, "/admin/robots", http.StatusSeeOther)
}

// handleAdminGrantRobotAccess gives a robot a role on an organization or a
// project, from the robots page rather than by typing its username into the
// project's own grant form.
func (h *Handler) handleAdminGrantRobotAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	robotID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid robot ID", http.StatusBadRequest)
		return
	}
	robot, err := h.users.GetByID(ctx, robotID)
	if err != nil || !robot.IsRobot {
		http.Error(w, "Robot not found", http.StatusNotFound)
		return
	}

	if problem := h.grantRobotScope(ctx, robotID, r.FormValue("scope"), r.FormValue("role")); problem != "" {
		h.robotError(w, r, problem)
		return
	}
	h.redirect(w, r, "/admin/robots?msg=granted", http.StatusSeeOther)
}

func (h *Handler) handleAdminRevokeRobotAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	grantID, err := strconv.ParseInt(r.PathValue("grantID"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid grant", http.StatusBadRequest)
		return
	}

	// Revoke reports whether a row actually went, so a click that matched
	// nothing says so rather than redirecting as though it worked (#126).
	removed, err := h.accessGrants.Revoke(ctx, grantID)
	if err != nil {
		h.logger.Error("revoking robot access", "grant_id", grantID, "error", err)
		h.robotError(w, r, "Could not revoke that access.")
		return
	}
	if !removed {
		h.robotError(w, r, "That grant no longer exists.")
		return
	}
	h.redirect(w, r, "/admin/robots?msg=revoked", http.StatusSeeOther)
}

// grantRobotScope applies one "org:12" or "project:7" scope string. It returns
// a message for the operator, or "" when the grant landed.
func (h *Handler) grantRobotScope(ctx context.Context, robotID int64, scope, role string) string {
	if !database.ValidGrantRole(role) {
		return "Unknown role."
	}

	kind, rest, ok := strings.Cut(scope, ":")
	if !ok {
		return "Choose an organization or a project to grant on."
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return "Choose an organization or a project to grant on."
	}

	grant := &database.AccessGrant{UserID: &robotID, Role: role}
	switch kind {
	case "org":
		if _, err := h.orgs.GetByID(ctx, id); err != nil {
			return "That organization no longer exists."
		}
		grant.OrgID = &id
	case "project":
		if _, err := h.projects.GetByID(ctx, id); err != nil {
			return "That project no longer exists."
		}
		grant.ProjectID = &id
	default:
		return "Choose an organization or a project to grant on."
	}

	if err := h.accessGrants.Grant(ctx, grant); err != nil {
		h.logger.Error("granting robot access", "robot_id", robotID, "error", err)
		return "Could not grant that access."
	}
	return ""
}

func (h *Handler) robotError(w http.ResponseWriter, r *http.Request, message string) {
	h.redirect(w, r, "/admin/robots?msg=error&error="+url.QueryEscape(message), http.StatusSeeOther)
}

func (h *Handler) handleAdminGenerateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	expiresAt, problem := expiryFromForm(r.FormValue("expires_in_days"))
	if problem != "" {
		h.robotError(w, r, problem)
		return
	}

	// Generate raw token
	rawToken, err := auth.GenerateToken(32)
	if err != nil {
		h.logger.Error("generating token", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	token := &database.APIToken{
		UserID:    robotID,
		ProjectID: projectID,
		TokenHash: auth.HashToken(rawToken),
		Name:      name,
		Scopes:    scopesFromForm(r.FormValue("may_create")),
		ExpiresAt: expiresAt,
	}

	if err := h.tokens.Create(ctx, token); err != nil {
		h.logger.Error("creating token", "error", err)
		http.Error(w, "Failed to create token", http.StatusInternalServerError)
		return
	}

	// Re-render with the new token shown: it is the only time anyone sees it.
	h.renderRobots(w, r, rawToken)
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

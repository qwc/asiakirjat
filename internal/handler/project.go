package handler

import (
	"context"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
	"github.com/qwc/asiakirjat/internal/validation"
)

// attachmentDisposition builds a safe `attachment; filename=...` header value.
// mime.FormatMediaType properly quotes the filename and falls back to a sanitized
// value if the inputs contain characters that can't be represented; a plain-ASCII
// fallback is used when that happens so the header is always valid.
func attachmentDisposition(slug, tag, ext string) string {
	filename := slug + "-" + tag + "." + ext
	v := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if v == "" {
		safe := strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
				return r
			}
			return '_'
		}, filename)
		v = `attachment; filename="` + safe + `"`
	}
	return v
}

type versionViewData struct {
	Tag         string
	URL         string
	CreatedAt   interface{ Format(string) string }
	ProjectSlug string
	IsPDF       bool
	// ExpiresIn is empty for a version retention keeps; otherwise it reads
	// "in 12 days" or "soon" (issue #149). ExpiresAt is the due date, shown
	// as the badge's tooltip.
	ExpiresIn string
	ExpiresAt string
}

func (h *Handler) handleProjectDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Access check
	if !h.canViewProject(ctx, user, project) {
		if user == nil {
			h.redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	versions, err := h.versions.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error("listing versions", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Sort versions by semver descending
	tags := make([]string, len(versions))
	versionMap := make(map[string]int)
	for i, v := range versions {
		tags[i] = v.Tag
		versionMap[v.Tag] = i
	}
	docs.SortVersionTags(tags)

	expiries := h.versionExpiries(project, versions)

	var versionViews []versionViewData
	bp := h.config.Server.BasePath
	for _, tag := range tags {
		v := versions[versionMap[tag]]
		expiry := expiries[v.Tag]
		versionViews = append(versionViews, versionViewData{
			Tag:         v.Tag,
			URL:         bp + "/project/" + slug + "/" + v.Tag + "/",
			CreatedAt:   v.CreatedAt,
			ProjectSlug: slug,
			IsPDF:       v.ContentType == "pdf",
			ExpiresIn:   expiry.In,
			ExpiresAt:   expiry.At,
		})
	}

	canUpload := h.canUpload(ctx, user, project)

	// Determine the computed latest version (by semver sort)
	latestVersion := ""
	if len(tags) > 0 {
		latestVersion = tags[0]
	}

	// If a version is pinned, use it as latest (if it exists)
	effectiveLatest := latestVersion
	if project.PinnedVersion != nil {
		for _, tag := range tags {
			if tag == *project.PinnedVersion {
				effectiveLatest = *project.PinnedVersion
				break
			}
		}
	}

	// Build base URL for API examples
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := scheme + "://" + r.Host

	data := map[string]any{
		"User":            user,
		"Project":         project,
		"Versions":        versionViews,
		"CanUpload":       canUpload,
		"CanDelete":       canUpload,
		"BaseURL":         baseURL,
		"PinnedVersion":   project.PinnedVersion,
		"PinPermanent":    project.PinPermanent,
		"LatestVersion":   latestVersion,
		"EffectiveLatest": effectiveLatest,
	}

	// The rule behind the expiry badges, stated once above the list. Zero days
	// means retention is off, and the template then says nothing.
	if days := h.effectiveRetentionDays(project); days > 0 {
		pattern, _ := h.effectiveKeepPattern(project)
		data["RetentionDays"] = days
		data["RetentionPattern"] = pattern
	}

	// Fetch upload logs for editors/admins
	if canUpload && h.uploadLogs != nil {
		logs, err := h.uploadLogs.ListByProject(ctx, project.ID)
		if err != nil {
			h.logger.Error("listing upload logs", "error", err)
		} else {
			// Build user lookup
			users, _ := h.users.List(ctx)
			userNames := make(map[int64]string)
			for _, u := range users {
				userNames[u.ID] = u.Username
			}

			type logView struct {
				VersionTag  string
				ContentType string
				Username    string
				IsReupload  bool
				Filename    string
				CreatedAt   interface{ Format(string) string }
			}

			var logViews []logView
			for _, l := range logs {
				username := "(deleted user)"
				if l.UploadedBy != nil {
					if name, ok := userNames[*l.UploadedBy]; ok {
						username = name
					}
				}
				logViews = append(logViews, logView{
					VersionTag:  l.VersionTag,
					ContentType: l.ContentType,
					Username:    username,
					IsReupload:  l.IsReupload,
					Filename:    l.Filename,
					CreatedAt:   l.CreatedAt,
				})
			}
			data["UploadLogs"] = logViews
		}
	}

	h.render(w, r, "project_detail", data)
}

func (h *Handler) handleDeleteVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")
	tag := r.PathValue("tag")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	version, err := h.versions.GetByProjectAndTag(ctx, project.ID, tag)
	if err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	// Delete from database
	if err := h.versions.Delete(ctx, version.ID); err != nil {
		h.logger.Error("deleting version from database", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Delete from filesystem
	if err := h.storage.DeleteVersion(slug, tag); err != nil {
		h.logger.Error("deleting version from filesystem", "error", err)
		// Continue - database record is already deleted
	}

	// Delete from search index
	if h.searchIndex != nil {
		if err := h.searchIndex.DeleteVersion(project.ID, version.ID); err != nil {
			h.logger.Error("deleting version from search index", "error", err)
			// Continue - not critical
		}
	}

	// Invalidate latest tags cache
	h.invalidateLatestTagsCache()

	h.logger.Info("version deleted", "project", slug, "version", tag, "user", user.Username)
	h.redirect(w, r, "/project/"+slug, http.StatusSeeOther)
}

func (h *Handler) handleDownloadVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")
	tag := r.PathValue("tag")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if !h.canViewProject(ctx, user, project) {
		if user == nil {
			h.redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	ver, err := h.versions.GetByProjectAndTag(ctx, project.ID, tag)
	if err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	versionPath := h.storage.VersionPath(slug, tag)
	if !h.storage.VersionExists(slug, tag) {
		http.Error(w, "Version files not found", http.StatusNotFound)
		return
	}

	if ver.ContentType == "pdf" {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", attachmentDisposition(slug, tag, "pdf"))
		http.ServeFile(w, r, filepath.Join(versionPath, "document.pdf"))
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", attachmentDisposition(slug, tag, "zip"))

	if err := docs.WriteZipFromDir(w, versionPath); err != nil {
		h.logger.Error("streaming version zip", "project", slug, "version", tag, "error", err)
	}
}

// handleProjectTokens lists API tokens scoped to this project.
// projectTokenView is one token row: the credential's name, and the robot it
// speaks for.
type projectTokenView struct {
	database.APIToken
	Username string
	IsRobot  bool
}

// renderProjectTokens draws the page for every handler that changes it.
func (h *Handler) renderProjectTokens(w http.ResponseWriter, r *http.Request, project *database.Project, newToken string, flash *Flash) {
	ctx := r.Context()

	tokens, err := h.tokens.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error("listing project tokens", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	users, _ := h.users.List(ctx)
	byID := make(map[int64]database.User, len(users))
	var robots []database.User
	for _, u := range users {
		byID[u.ID] = u
		if u.IsRobot {
			robots = append(robots, u)
		}
	}

	views := make([]projectTokenView, 0, len(tokens))
	for _, t := range tokens {
		owner := byID[t.UserID]
		views = append(views, projectTokenView{
			APIToken: t, Username: owner.Username, IsRobot: owner.IsRobot,
		})
	}

	data := map[string]any{
		"User":           auth.UserFromContext(ctx),
		"Project":        project,
		"Tokens":         views,
		"Robots":         robots,
		"SuggestedRobot": project.Slug + "-bot",
	}
	if newToken != "" {
		data["NewToken"] = newToken
	}
	if flash != nil {
		data["Flash"] = flash
	}
	h.render(w, r, "project_tokens", data)
}

func (h *Handler) handleProjectTokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	project, err := h.projects.GetBySlug(ctx, r.PathValue("slug"))
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	h.renderProjectTokens(w, r, project, "", nil)
}

// handleProjectCreateToken issues a token for this project, owned by a robot.
//
// It used to hang the token off whoever clicked the button, which made a
// project's CI credential a slice of one person's account: it carried their
// access, it said their name on every version it uploaded, and it died with
// them when the account went. A token names a robot now — an existing one, or
// one created here — and the robot is granted editor on this project, so its
// reach is a grant like everyone else's and the token's scope narrows it to
// this project (#155).
func (h *Handler) handleProjectCreateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		name = "default"
	}

	robotName := strings.TrimSpace(r.FormValue("robot"))
	if robotName == "" {
		robotName = slug + "-bot"
	}

	robot, problem := h.robotForProject(ctx, robotName, project)
	if problem != "" {
		h.renderProjectTokens(w, r, project, "", &Flash{Type: "error", Message: problem})
		return
	}

	rawToken, err := auth.GenerateToken(32)
	if err != nil {
		h.logger.Error("generating token", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	projectID := project.ID
	token := &database.APIToken{
		UserID:    robot.ID,
		ProjectID: &projectID,
		TokenHash: auth.HashToken(rawToken),
		Name:      name,
		Scopes:    "upload",
	}
	if err := h.tokens.Create(ctx, token); err != nil {
		h.logger.Error("creating token", "error", err)
		http.Error(w, "Failed to create token", http.StatusInternalServerError)
		return
	}

	h.renderProjectTokens(w, r, project, rawToken, nil)
}

// robotForProject finds or creates the robot a project token speaks for, and
// makes sure it may upload here. Reusing an existing robot is the common case
// — one robot, one token per pipeline — so a name that already belongs to a
// robot is not an error; a name that belongs to a person is.
func (h *Handler) robotForProject(ctx context.Context, name string, project *database.Project) (*database.User, string) {
	// A robot name is a slug: lowercase, hyphenated, no spaces. It ends up in
	// grant tables and log lines, and "CI Bot (temp)" helps nobody there.
	if !validation.IsValidSlug(name) {
		return nil, "A robot name must be lowercase letters, digits and hyphens."
	}

	robot, err := h.users.GetByUsername(ctx, name)
	if err == nil && robot != nil {
		if !robot.IsRobot {
			return nil, name + " is a person's account, not a robot. Choose another name."
		}
	} else {
		robot = &database.User{
			Username: name, AuthSource: "robot", Role: "viewer", IsRobot: true,
		}
		if err := h.users.Create(ctx, robot); err != nil {
			h.logger.Error("creating robot for project token", "username", name, "error", err)
			return nil, "Could not create the robot " + name + "."
		}
	}

	// Grant is idempotent on (subject, scope), so re-issuing a token for the
	// same robot updates nothing rather than piling up rows.
	if err := h.accessGrants.Grant(ctx, &database.AccessGrant{
		UserID: &robot.ID, ProjectID: &project.ID, Role: database.GrantRoleEditor,
	}); err != nil {
		h.logger.Error("granting robot access to project", "username", name, "error", err)
		return nil, "Could not give " + name + " access to this project."
	}
	return robot, ""
}

// handleProjectRevokeToken revokes a token scoped to this project.
func (h *Handler) handleProjectRevokeToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// Check editor access
	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	tokenID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	// Validate token belongs to this project
	token, err := h.tokens.GetByID(ctx, tokenID)
	if err != nil {
		http.Error(w, "Token not found", http.StatusNotFound)
		return
	}
	if token.ProjectID == nil || *token.ProjectID != project.ID {
		http.Error(w, "Token does not belong to this project", http.StatusForbidden)
		return
	}

	if err := h.tokens.Delete(ctx, tokenID); err != nil {
		h.logger.Error("revoking token", "error", err)
		http.Error(w, "Failed to revoke token", http.StatusInternalServerError)
		return
	}

	h.redirect(w, r, "/project/"+slug+"/tokens", http.StatusSeeOther)
}

// handlePinVersion pins a version as the "latest" for a project.
func (h *Handler) handlePinVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")
	tag := r.PathValue("tag")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Verify the version exists
	if _, err := h.versions.GetByProjectAndTag(ctx, project.ID, tag); err != nil {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	permanent := r.FormValue("permanent") == "true"
	project.PinnedVersion = &tag
	project.PinPermanent = permanent

	if err := h.projects.Update(ctx, project); err != nil {
		h.logger.Error("pinning version", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.invalidateLatestTagsCache()
	h.logger.Info("version pinned", "project", slug, "version", tag, "permanent", permanent, "user", user.Username)
	h.redirect(w, r, "/project/"+slug, http.StatusSeeOther)
}

// handleUnpinVersion removes the pinned version from a project.
func (h *Handler) handleUnpinVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	slug := r.PathValue("slug")

	project, err := h.projects.GetBySlug(ctx, slug)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if !h.canUpload(ctx, user, project) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	project.PinnedVersion = nil
	project.PinPermanent = false

	if err := h.projects.Update(ctx, project); err != nil {
		h.logger.Error("unpinning version", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.invalidateLatestTagsCache()
	h.logger.Info("version unpinned", "project", slug, "user", user.Username)
	h.redirect(w, r, "/project/"+slug, http.StatusSeeOther)
}

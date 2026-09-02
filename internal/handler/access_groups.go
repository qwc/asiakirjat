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

// Access groups and the grants that use them (issues #150, #151).
//
// One page manages who exists as a set of people; the grant tables on a
// project or an org decide what those sets can do. Splitting it that way is
// the whole simplification: before, four pages each described both at once in
// slightly different words.

// accessGroupView carries a group with what the page shows about it.
type accessGroupView struct {
	Group      database.AccessGroup
	Members    []database.AccessGroupMember
	GrantCount int
}

func (h *Handler) handleAdminAccessGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	groups, err := h.accessGroups.List(ctx)
	if err != nil {
		h.logger.Error("listing access groups", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	views := make([]accessGroupView, 0, len(groups))
	for _, g := range groups {
		members, err := h.accessGroups.ListMembers(ctx, g.ID)
		if err != nil {
			h.logger.Error("listing access group members", "group_id", g.ID, "error", err)
		}
		count, err := h.accessGroups.CountGrants(ctx, g.ID)
		if err != nil {
			h.logger.Error("counting grants for access group", "group_id", g.ID, "error", err)
		}
		views = append(views, accessGroupView{Group: g, Members: members, GrantCount: count})
	}

	data := map[string]any{
		"User":   auth.UserFromContext(ctx),
		"Groups": views,
	}
	applyFlash(data, r, map[string]string{
		"created":        "Access group created.",
		"updated":        "Access group updated.",
		"deleted":        "Access group deleted.",
		"member_added":   "Member added.",
		"member_removed": "Member removed.",
	})
	h.render(w, r, "admin_access_groups", data)
}

func (h *Handler) handleAdminCreateAccessGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.accessGroupError(w, r, "A group needs a name.")
		return
	}

	group := &database.AccessGroup{Name: name, Description: strings.TrimSpace(r.FormValue("description"))}
	if err := h.accessGroups.Create(ctx, group); err != nil {
		h.logger.Error("creating access group", "name", name, "error", err)
		h.accessGroupError(w, r, "Could not create that group. The name may already be taken.")
		return
	}
	h.redirect(w, r, "/admin/access-groups?msg=created", http.StatusSeeOther)
}

// handleAdminUpdateAccessGroup renames a group or edits its description
// (issue #151). The grants that name it are unaffected: they point at the
// group's id, not its name.
func (h *Handler) handleAdminUpdateAccessGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid group", http.StatusBadRequest)
		return
	}
	group, err := h.accessGroups.GetByID(ctx, id)
	if err != nil {
		http.Error(w, "Group not found", http.StatusNotFound)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.accessGroupError(w, r, "A group needs a name.")
		return
	}
	group.Name = name
	group.Description = strings.TrimSpace(r.FormValue("description"))

	if err := h.accessGroups.Update(ctx, group); err != nil {
		h.logger.Error("updating access group", "group_id", id, "error", err)
		h.accessGroupError(w, r, "Could not rename that group. The name may already be taken.")
		return
	}
	h.redirect(w, r, "/admin/access-groups?msg=updated", http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteAccessGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid group", http.StatusBadRequest)
		return
	}
	if err := h.accessGroups.Delete(ctx, id); err != nil {
		h.logger.Error("deleting access group", "group_id", id, "error", err)
		h.accessGroupError(w, r, "Could not delete that group.")
		return
	}
	h.redirect(w, r, "/admin/access-groups?msg=deleted", http.StatusSeeOther)
}

func (h *Handler) handleAdminAddAccessGroupMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	groupID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid group", http.StatusBadRequest)
		return
	}

	subjectType := r.FormValue("subject_type")
	identifier := strings.TrimSpace(r.FormValue("subject_identifier"))
	if identifier == "" {
		h.accessGroupError(w, r, "A member needs an identifier.")
		return
	}

	// The store validates the subject type too; refusing here keeps the
	// message useful instead of surfacing a database error.
	if !database.ValidSubjectType(subjectType) {
		h.accessGroupError(w, r, "Unknown member type.")
		return
	}

	member := &database.AccessGroupMember{GroupID: groupID, SubjectType: subjectType, SubjectIdentifier: identifier}
	if err := h.accessGroups.AddMember(ctx, member); err != nil {
		h.logger.Error("adding access group member", "group_id", groupID, "error", err)
		h.accessGroupError(w, r, "Could not add that member. They may already be in the group.")
		return
	}
	h.redirect(w, r, "/admin/access-groups?msg=member_added", http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteAccessGroupMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	memberID, err := strconv.ParseInt(r.PathValue("memberID"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid member", http.StatusBadRequest)
		return
	}
	if err := h.accessGroups.RemoveMember(ctx, memberID); err != nil {
		h.logger.Error("removing access group member", "member_id", memberID, "error", err)
		h.accessGroupError(w, r, "Could not remove that member.")
		return
	}
	h.redirect(w, r, "/admin/access-groups?msg=member_removed", http.StatusSeeOther)
}

func (h *Handler) accessGroupError(w http.ResponseWriter, r *http.Request, message string) {
	h.redirect(w, r, "/admin/access-groups?msg=error&error="+url.QueryEscape(message), http.StatusSeeOther)
}

// applyFlash turns a ?msg= marker into the banner the page renders. Every
// admin page in this package spells the same switch out by hand; this is the
// one shared copy.
func applyFlash(data map[string]any, r *http.Request, messages map[string]string) {
	msg := r.URL.Query().Get("msg")
	if msg == "error" {
		data["Flash"] = &Flash{Type: "error", Message: r.URL.Query().Get("error")}
		return
	}
	if text, ok := messages[msg]; ok {
		data["Flash"] = &Flash{Type: "success", Message: text}
	}
}

// grantView flattens one grant for display: templates cannot dereference the
// nullable subject columns, and the subject's name needs a lookup either way.
type grantView struct {
	ID      int64
	Kind    string // "group" or "user"
	Subject string
	Role    string
}

// grantViews resolves a set of grants into displayable rows, naming the group
// or user each one points at.
func (h *Handler) grantViews(ctx context.Context, grants []database.AccessGrant) []grantView {
	views := make([]grantView, 0, len(grants))
	for _, g := range grants {
		switch {
		case g.GroupID != nil:
			name := "(deleted group)"
			if group, err := h.accessGroups.GetByID(ctx, *g.GroupID); err == nil {
				name = group.Name
			}
			views = append(views, grantView{ID: g.ID, Kind: "group", Subject: name, Role: g.Role})
		case g.UserID != nil:
			name := "(deleted user)"
			if user, err := h.users.GetByID(ctx, *g.UserID); err == nil {
				name = user.Username
			}
			views = append(views, grantView{ID: g.ID, Kind: "user", Subject: name, Role: g.Role})
		}
	}
	return views
}

// grantFromForm builds a grant from an admin form's subject fields. scope is
// applied by the caller, which knows whether it is granting on a project or
// an org.
func (h *Handler) grantFromForm(ctx context.Context, r *http.Request) (*database.AccessGrant, string) {
	subject := strings.TrimSpace(r.FormValue("subject"))
	if subject == "" {
		return nil, "Name a group or user to grant access to."
	}
	role := r.FormValue("role")
	if !database.ValidGrantRole(role) {
		return nil, "Unknown role."
	}

	grant := &database.AccessGrant{Role: role}
	switch r.FormValue("subject_kind") {
	case "group":
		group, err := h.accessGroups.GetByName(ctx, subject)
		if err != nil {
			return nil, "No access group called " + subject + "."
		}
		grant.GroupID = &group.ID
	case "user":
		user, err := h.users.GetByUsername(ctx, subject)
		if err != nil || user == nil {
			return nil, "No user called " + subject + "."
		}
		grant.UserID = &user.ID
	default:
		return nil, "Choose whether to grant to a group or a user."
	}
	return grant, ""
}

// retiredAccessPage sends one of the replaced access pages to whatever now
// does its job.
//
// The pages are not merely hidden from the nav: their old addresses are
// bookmarked and linked from the docs, and landing on a form that saves into a
// table nothing reads would be worse than a redirect. The rows themselves are
// untouched — the migration read them, and they stay until it is confirmed
// good in production.
func (h *Handler) retiredAccessPage(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.redirect(w, r, target+"?msg=error&error="+url.QueryEscape(
			"That page has been replaced. Access is managed with groups and grants now; "+
				"your existing configuration was migrated automatically."), http.StatusSeeOther)
	}
}

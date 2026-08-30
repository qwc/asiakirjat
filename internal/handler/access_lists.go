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

// Named access lists (issue #125): a reusable set of subjects — LDAP groups,
// OAuth2 groups, individual users, or a mix — that projects can point at via
// the 'list' visibility instead of repeating per-project grants.
//
// These live outside admin.go, which the audit already flags as overgrown
// (M-11) and which two recent bugs were traced through.

// accessListView carries a list together with the details the page shows
// about it: who is in it, and how many projects it governs.
type accessListView struct {
	List         database.AccessList
	Members      []database.AccessListMember
	ProjectCount int
}

func (h *Handler) handleAdminAccessLists(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	lists, err := h.accessLists.List(ctx)
	if err != nil {
		h.logger.Error("listing access lists", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	views := make([]accessListView, 0, len(lists))
	for _, l := range lists {
		members, err := h.accessLists.ListMembers(ctx, l.ID)
		if err != nil {
			h.logger.Error("listing access list members", "list_id", l.ID, "error", err)
		}
		count, err := h.accessLists.CountProjectsUsing(ctx, l.ID)
		if err != nil {
			h.logger.Error("counting projects using access list", "list_id", l.ID, "error", err)
		}
		views = append(views, accessListView{List: l, Members: members, ProjectCount: count})
	}

	data := map[string]any{
		"User":  user,
		"Lists": views,
	}
	switch r.URL.Query().Get("msg") {
	case "created":
		data["Flash"] = &Flash{Type: "success", Message: "Access list created."}
	case "deleted":
		data["Flash"] = &Flash{Type: "success", Message: "Access list deleted."}
	case "member_added":
		data["Flash"] = &Flash{Type: "success", Message: "Member added."}
	case "member_removed":
		data["Flash"] = &Flash{Type: "success", Message: "Member removed."}
	case "error":
		data["Flash"] = &Flash{Type: "error", Message: r.URL.Query().Get("error")}
	}

	h.render(w, r, "admin_access_lists", data)
}

func (h *Handler) handleAdminCreateAccessList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.accessListError(w, r, "Name is required")
		return
	}

	list := &database.AccessList{
		Name:        name,
		Description: strings.TrimSpace(r.FormValue("description")),
	}
	if err := h.accessLists.Create(ctx, list); err != nil {
		h.logger.Error("creating access list", "error", err)
		h.accessListError(w, r, "Failed to create list — is the name already taken?")
		return
	}

	h.redirect(w, r, "/admin/access-lists?msg=created", http.StatusSeeOther)
}

// handleAdminDeleteAccessList refuses to delete a list that projects still
// point at. The FK would reject it anyway (ON DELETE RESTRICT); checking
// first lets the admin see which projects are in the way instead of a
// constraint error.
func (h *Handler) handleAdminDeleteAccessList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid list ID", http.StatusBadRequest)
		return
	}

	count, err := h.accessLists.CountProjectsUsing(ctx, id)
	if err != nil {
		h.logger.Error("counting projects using access list", "list_id", id, "error", err)
		h.accessListError(w, r, "Failed to delete list")
		return
	}
	if count > 0 {
		h.accessListError(w, r, "This list still governs "+strconv.Itoa(count)+
			" project(s). Change their visibility first.")
		return
	}

	if err := h.accessLists.Delete(ctx, id); err != nil {
		h.logger.Error("deleting access list", "list_id", id, "error", err)
		h.accessListError(w, r, "Failed to delete list")
		return
	}

	h.redirect(w, r, "/admin/access-lists?msg=deleted", http.StatusSeeOther)
}

func (h *Handler) handleAdminAddAccessListMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid list ID", http.StatusBadRequest)
		return
	}

	subjectType := r.FormValue("subject_type")
	identifier := strings.TrimSpace(r.FormValue("subject_identifier"))
	role := r.FormValue("role")

	if !database.ValidSubjectType(subjectType) {
		h.accessListError(w, r, "Invalid subject type")
		return
	}
	if identifier == "" {
		h.accessListError(w, r, "Identifier is required")
		return
	}
	if !database.ValidAccessRole(role) {
		role = "viewer"
	}

	member := &database.AccessListMember{
		ListID:            id,
		SubjectType:       subjectType,
		SubjectIdentifier: identifier,
		Role:              role,
	}
	if err := h.accessLists.AddMember(ctx, member); err != nil {
		h.logger.Error("adding access list member", "list_id", id, "error", err)
		h.accessListError(w, r, "Failed to add member")
		return
	}

	h.redirect(w, r, "/admin/access-lists?msg=member_added", http.StatusSeeOther)
}

func (h *Handler) handleAdminDeleteAccessListMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	memberID, err := strconv.ParseInt(r.PathValue("memberID"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	if err := h.accessLists.RemoveMember(ctx, memberID); err != nil {
		h.logger.Error("removing access list member", "member_id", memberID, "error", err)
		h.accessListError(w, r, "Failed to remove member")
		return
	}

	h.redirect(w, r, "/admin/access-lists?msg=member_removed", http.StatusSeeOther)
}

// accessListError redirects back to the page with a message, matching how the
// global access handlers report problems.
func (h *Handler) accessListError(w http.ResponseWriter, r *http.Request, message string) {
	h.redirect(w, r, "/admin/access-lists?msg=error&error="+url.QueryEscape(message), http.StatusSeeOther)
}

// accessListIDFromForm reads the list a project is being pointed at. It
// returns nil for every other visibility, so switching a project away from a
// list clears the pointer rather than leaving a stale one behind.
func accessListIDFromForm(r *http.Request, visibility string) *int64 {
	if visibility != database.VisibilityList {
		return nil
	}
	id, err := strconv.ParseInt(r.FormValue("access_list_id"), 10, 64)
	if err != nil {
		return nil
	}
	return &id
}

// availableAccessLists returns the lists a visibility picker can offer. An
// error is logged and treated as "none", which hides the option rather than
// failing the page.
func (h *Handler) availableAccessLists(ctx context.Context) []database.AccessList {
	if h.accessLists == nil {
		return nil
	}
	lists, err := h.accessLists.List(ctx)
	if err != nil {
		h.logger.Error("listing access lists for picker", "error", err)
		return nil
	}
	return lists
}

// currentAccessListID flattens the project's list pointer for the template,
// which cannot dereference one. 0 means "no list", matching no real row.
func currentAccessListID(project *database.Project) int64 {
	if project.AccessListID == nil {
		return 0
	}
	return *project.AccessListID
}

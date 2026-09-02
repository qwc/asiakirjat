package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/validation"
)

// Organizations (issue #150): the container above projects, and a scope you
// can grant on. A role held on an org applies to every project in it, which is
// what makes an org an access boundary rather than a label.
//
// Orgs never appear in URLs. Project slugs stay globally unique, so an org
// slug cannot collide with a project's, and no existing link breaks.

type orgView struct {
	Org          database.Org
	ProjectCount int
	Grants       []grantView
}

func (h *Handler) handleAdminOrgs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	orgs, err := h.orgs.List(ctx)
	if err != nil {
		h.logger.Error("listing orgs", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	views := make([]orgView, 0, len(orgs))
	for _, o := range orgs {
		count, err := h.orgs.CountProjects(ctx, o.ID)
		if err != nil {
			h.logger.Error("counting projects in org", "org_id", o.ID, "error", err)
		}
		grants, err := h.accessGrants.ListByOrg(ctx, o.ID)
		if err != nil {
			h.logger.Error("listing org grants", "org_id", o.ID, "error", err)
		}
		views = append(views, orgView{Org: o, ProjectCount: count, Grants: h.grantViews(ctx, grants)})
	}

	data := map[string]any{
		"User":   auth.UserFromContext(ctx),
		"Orgs":   views,
		"Groups": h.availableAccessGroups(ctx),
	}
	applyFlash(data, r, map[string]string{
		"created":   "Organization created.",
		"updated":   "Organization updated.",
		"deleted":   "Organization deleted.",
		"granted":   "Access granted.",
		"revoked":   "Access revoked.",
		"unchanged": "That grant was already in place.",
	})
	h.render(w, r, "admin_orgs", data)
}

func (h *Handler) handleAdminCreateOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := strings.TrimSpace(r.FormValue("name"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	if name == "" {
		h.orgError(w, r, "An organization needs a name.")
		return
	}
	if slug == "" {
		slug = validation.Slugify(name)
	}
	if !validation.IsValidSlug(slug) {
		h.orgError(w, r, "That slug is not valid. Use lowercase letters, digits and dashes.")
		return
	}

	org := &database.Org{Slug: slug, Name: name, Description: strings.TrimSpace(r.FormValue("description"))}
	if err := h.orgs.Create(ctx, org); err != nil {
		h.logger.Error("creating org", "slug", slug, "error", err)
		h.orgError(w, r, "Could not create that organization. The slug may already be taken.")
		return
	}
	h.redirect(w, r, "/admin/orgs?msg=created", http.StatusSeeOther)
}

func (h *Handler) handleAdminUpdateOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid organization", http.StatusBadRequest)
		return
	}
	org, err := h.orgs.GetByID(ctx, id)
	if err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.orgError(w, r, "An organization needs a name.")
		return
	}
	org.Name = name
	org.Description = strings.TrimSpace(r.FormValue("description"))

	if err := h.orgs.Update(ctx, org); err != nil {
		h.logger.Error("updating org", "org_id", id, "error", err)
		h.orgError(w, r, "Could not update that organization.")
		return
	}
	h.redirect(w, r, "/admin/orgs?msg=updated", http.StatusSeeOther)
}

// handleAdminDeleteOrg removes an empty org. The store refuses while it still
// holds projects, and says how many, rather than orphaning them.
func (h *Handler) handleAdminDeleteOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid organization", http.StatusBadRequest)
		return
	}
	if err := h.orgs.Delete(ctx, id); err != nil {
		h.logger.Warn("deleting org", "org_id", id, "error", err)
		h.orgError(w, r, "Could not delete that organization: "+err.Error())
		return
	}
	h.redirect(w, r, "/admin/orgs?msg=deleted", http.StatusSeeOther)
}

func (h *Handler) handleAdminGrantOrgAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid organization", http.StatusBadRequest)
		return
	}

	grant, problem := h.grantFromForm(ctx, r)
	if problem != "" {
		h.orgError(w, r, problem)
		return
	}
	grant.OrgID = &id

	if err := h.accessGrants.Grant(ctx, grant); err != nil {
		h.logger.Error("granting org access", "org_id", id, "error", err)
		h.orgError(w, r, "Could not grant that access.")
		return
	}
	h.redirect(w, r, "/admin/orgs?msg=granted", http.StatusSeeOther)
}

func (h *Handler) handleAdminRevokeOrgAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	grantID, err := strconv.ParseInt(r.PathValue("grantID"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid grant", http.StatusBadRequest)
		return
	}

	// Revoke reports whether a row actually went, so a click that matched
	// nothing says so instead of redirecting as though it worked (issue #126).
	removed, err := h.accessGrants.Revoke(ctx, grantID)
	if err != nil {
		h.logger.Error("revoking org access", "grant_id", grantID, "error", err)
		h.orgError(w, r, "Could not revoke that access.")
		return
	}
	if !removed {
		h.orgError(w, r, "That grant no longer exists.")
		return
	}
	h.redirect(w, r, "/admin/orgs?msg=revoked", http.StatusSeeOther)
}

func (h *Handler) orgError(w http.ResponseWriter, r *http.Request, message string) {
	h.redirect(w, r, "/admin/orgs?msg=error&error="+url.QueryEscape(message), http.StatusSeeOther)
}

// availableAccessGroups lists groups for the grant forms' pickers.
func (h *Handler) availableAccessGroups(ctx context.Context) []database.AccessGroup {
	groups, err := h.accessGroups.List(ctx)
	if err != nil {
		h.logger.Error("listing access groups", "error", err)
		return nil
	}
	return groups
}

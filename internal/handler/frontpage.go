package handler

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/qwc/asiakirjat/internal/auth"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/docs"
)

type projectCardData struct {
	Name          string
	Slug          string
	Description   string
	Visibility    string
	OrgName       string
	LatestVersion string
}

// orgGroup is one organization's projects on the frontpage. Grouping happens
// here rather than in the template so the ordering rule lives in one place:
// the default org first, because it holds everything on an installation that
// has not started using organizations yet, then the rest by name.
type orgGroup struct {
	Name     string
	Projects []projectCardData
}

// latestVersionTag returns the "latest" version tag.
// If pinnedVersion is set and exists in the list, it takes priority.
// Otherwise, falls back to the highest semver-sorted tag.
func latestVersionTag(versions []database.Version, pinnedVersion *string) string {
	if len(versions) == 0 {
		return ""
	}
	if pinnedVersion != nil {
		for _, v := range versions {
			if v.Tag == *pinnedVersion {
				return *pinnedVersion
			}
		}
	}
	tags := make([]string, len(versions))
	for i, v := range versions {
		tags[i] = v.Tag
	}
	docs.SortVersionTags(tags)
	return tags[0]
}

// filterAccessibleProjects is a thin forwarder to access.Checker.FilterAccessible;
// the rule lives in internal/access.
func (h *Handler) filterAccessibleProjects(ctx context.Context, user *database.User, all []database.Project) []database.Project {
	return h.resolver.FilterAccessible(ctx, user, all)
}

func (h *Handler) handleFrontpage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	// One path for everyone. The anonymous case used to select on the
	// visibility column, which the access model retired; FilterAccessible
	// answers it from exposure, and already returns only public projects for a
	// nil user.
	all, err := h.projects.List(ctx)
	if err != nil {
		h.logger.Error("listing projects", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	dbProjects := h.filterAccessibleProjects(ctx, user, all)

	orgNames := h.orgNamesByID(ctx)

	var projects []projectCardData
	for _, p := range dbProjects {
		card := projectCardData{
			Name:        p.Name,
			Slug:        p.Slug,
			Description: p.Description,
			Visibility:  p.Visibility,
		}
		if p.OrgID != nil {
			card.OrgName = orgNames[*p.OrgID]
		}
		versions, _ := h.versions.ListByProject(ctx, p.ID)
		card.LatestVersion = latestVersionTag(versions, p.PinnedVersion)
		projects = append(projects, card)
	}

	groups := groupByOrg(projects, h.defaultOrgName(ctx))

	h.render(w, r, "frontpage", map[string]any{
		"User":     user,
		"Projects": projects,
		"Groups":   groups,
		// A single group means organizations are not in use yet, and headers
		// for it would be noise on every page load.
		"ShowOrgGroups": len(groups) > 1,
	})
}

// orgNamesByID maps org ids to names for the cards. One lookup for the whole
// page rather than one per project.
func (h *Handler) orgNamesByID(ctx context.Context) map[int64]string {
	names := map[int64]string{}
	if h.orgs == nil {
		return names
	}
	orgs, err := h.orgs.List(ctx)
	if err != nil {
		h.logger.Error("listing orgs", "error", err)
		return names
	}
	for _, o := range orgs {
		names[o.ID] = o.Name
	}
	return names
}

// defaultOrgName is the name of the org that holds everything not placed
// elsewhere, so grouping can put it first whatever it has been renamed to.
func (h *Handler) defaultOrgName(ctx context.Context) string {
	if h.orgs == nil {
		return ""
	}
	org, err := h.orgs.GetBySlug(ctx, database.DefaultOrgSlug)
	if err != nil || org == nil {
		return ""
	}
	return org.Name
}

// groupByOrg buckets cards by organization: the default org first, then the
// rest by name, and each group's projects in the order they arrived (the store
// already sorts by name).
func groupByOrg(projects []projectCardData, defaultOrgName string) []orgGroup {
	byName := map[string][]projectCardData{}
	var order []string
	for _, p := range projects {
		if _, seen := byName[p.OrgName]; !seen {
			order = append(order, p.OrgName)
		}
		byName[p.OrgName] = append(byName[p.OrgName], p)
	}

	sort.Slice(order, func(i, j int) bool {
		if order[i] == defaultOrgName {
			return true
		}
		if order[j] == defaultOrgName {
			return false
		}
		return strings.ToLower(order[i]) < strings.ToLower(order[j])
	})

	groups := make([]orgGroup, 0, len(order))
	for _, name := range order {
		groups = append(groups, orgGroup{Name: name, Projects: byName[name]})
	}
	return groups
}

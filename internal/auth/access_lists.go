package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
)

// syncAccessListGrants reconciles one login source's access-list grants for a
// user against the groups that source just reported (issue #125).
//
// A list member naming a user is matched by username when access is checked
// and needs nothing here; only group members do, because group membership is
// known during sign-in and never persisted.
//
// Both authenticators share this rather than keeping a copy each: LDAP and
// OAuth2 differ only in the subject type they match and the source they
// record, and two copies of an authorization rule is how they drift.
func syncAccessListGrants(
	ctx context.Context,
	lists store.AccessListStore,
	logger *slog.Logger,
	user *database.User,
	groups []string,
	subjectType string,
	source string,
) error {
	members, err := lists.ListMembersBySubjectType(ctx, subjectType)
	if err != nil {
		return fmt.Errorf("listing access list members: %w", err)
	}

	// Group names are compared case-insensitively, as elsewhere in the
	// authenticators.
	userGroups := make(map[string]bool, len(groups))
	for _, g := range groups {
		userGroups[strings.ToLower(g)] = true
	}

	// Strongest role wins when several of a list's groups match.
	desired := make(map[int64]string)
	for _, m := range members {
		if !userGroups[strings.ToLower(m.SubjectIdentifier)] {
			continue
		}
		if roleHigher(m.Role, desired[m.ListID]) {
			desired[m.ListID] = m.Role
		}
	}

	existing, err := lists.ListGrantsByUserAndSource(ctx, user.ID, source)
	if err != nil {
		return fmt.Errorf("listing access list grants: %w", err)
	}
	existingRoles := make(map[int64]string, len(existing))
	for _, g := range existing {
		existingRoles[g.ListID] = g.Role
	}

	for listID, role := range desired {
		if existingRoles[listID] == role {
			continue
		}
		logger.Debug("granting access list membership",
			"username", user.Username, "list_id", listID, "role", role, "source", source)
		if err := lists.UpsertGrant(ctx, &database.AccessListGrant{
			ListID: listID, UserID: user.ID, Role: role, Source: source,
		}); err != nil {
			logger.Warn("granting access list membership", "list_id", listID, "error", err)
		}
	}

	// Reconcile rather than delete-then-recreate: a user who is still in the
	// group keeps an unbroken grant, with no window where they lose access
	// mid-login.
	for listID := range existingRoles {
		if _, keep := desired[listID]; keep {
			continue
		}
		logger.Debug("revoking access list membership",
			"username", user.Username, "list_id", listID, "source", source)
		if err := lists.DeleteGrant(ctx, listID, user.ID, source); err != nil {
			logger.Warn("revoking access list membership", "list_id", listID, "error", err)
		}
	}

	return nil
}

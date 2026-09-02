package auth

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
)

// syncAccessGroupMembership records which access groups a user belongs to
// according to the auth source they just signed in with (issues #150, #151).
//
// This is the whole login-time authorization job in the new model, and it is
// smaller than what it replaces because it decides nothing about access. It
// records one fact — "this user is in these groups" — and the grant edge turns
// that into roles at check time. Previously each source had to work out roles
// for projects, for global access and for access lists separately, from three
// tables that happened to hold the same shape.
//
// Members naming a user need nothing here: they are matched by username when
// access is checked, so naming someone works immediately rather than waiting
// for a login that may never come (the bug behind issue #135).
//
// LDAP and OAuth2 share this rather than keeping a copy each; two copies of an
// authorization rule is how they drift.
func syncAccessGroupMembership(
	ctx context.Context,
	groups store.AccessGroupStore,
	logger *slog.Logger,
	user *database.User,
	providerGroups []string,
	subjectType string,
	source string,
) error {
	// Matching is case-insensitive in the store: LDAP DNs and OAuth2 group
	// names arrive with inconsistent casing.
	seen := map[int64]bool{}
	var matched []int64
	for _, g := range providerGroups {
		ids, err := groups.ListGroupsBySubject(ctx, subjectType, g)
		if err != nil {
			return fmt.Errorf("resolving access groups for %q: %w", g, err)
		}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			matched = append(matched, id)
		}
	}

	logger.Debug("resolved access group membership",
		"username", user.Username, "source", source,
		"provider_groups", len(providerGroups), "access_groups", len(matched))

	// SetResolvedForUser reconciles rather than deleting and re-inserting, so a
	// user who is still in the group never loses access mid-login.
	if err := groups.SetResolvedForUser(ctx, user.ID, source, matched); err != nil {
		return fmt.Errorf("recording access group membership: %w", err)
	}
	return nil
}

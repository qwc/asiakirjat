package access

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store"
)

// ConfigSync applies the access declared in config.yaml to the database
// (issues #150, #151).
//
// What the file declares, the file owns: rows written from config carry
// source='config' and are reconciled against it on every startup, so removing
// an entry revokes it. Rows an admin added in the UI carry source='manual' and
// are never touched. Without that split, deleting a line from config would
// leave the access in place with nothing to say so — the silent no-op this
// whole redesign exists to end.
type ConfigSync struct {
	groups   store.AccessGroupStore
	grants   store.AccessGrantStore
	orgs     store.OrgStore
	projects store.ProjectStore
	users    store.UserStore
	logger   *slog.Logger
}

func NewConfigSync(
	groups store.AccessGroupStore,
	grants store.AccessGrantStore,
	orgs store.OrgStore,
	projects store.ProjectStore,
	users store.UserStore,
	logger *slog.Logger,
) *ConfigSync {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConfigSync{groups: groups, grants: grants, orgs: orgs, projects: projects, users: users, logger: logger}
}

// Apply reconciles the declared groups and grants. Individual bad entries are
// logged and skipped rather than aborting: one typo in a project slug should
// not stop the server, and a half-applied config is visible in the admin UI.
func (c *ConfigSync) Apply(ctx context.Context, cfg *config.Config) error {
	c.warnAboutRetiredKeys(cfg)

	declared := declaredGroups(cfg)
	if err := c.syncGroups(ctx, declared); err != nil {
		return err
	}
	return c.syncGrants(ctx, declaredGrants(cfg))
}

// warnAboutRetiredKeys reports config that no longer does anything.
//
// Silence would be the worst outcome: an operator whose config still says
// `auth.ldap.project_groups` deserves to be told where it went, not to
// discover months later that it stopped applying.
func (c *ConfigSync) warnAboutRetiredKeys(cfg *config.Config) {
	if n := len(cfg.Auth.LDAP.ProjectGroups) + len(cfg.Auth.OAuth2.ProjectGroups); n > 0 {
		c.logger.Warn("config: auth.*.project_groups is retired and is being applied as access groups and grants; move it to access.groups / access.grants",
			"entries", n)
	}

	p := cfg.Access.Private
	if n := len(p.Viewers.Users) + len(p.Viewers.LDAPGroups) + len(p.Viewers.OAuth2Groups) +
		len(p.Editors.Users) + len(p.Editors.LDAPGroups) + len(p.Editors.OAuth2Groups); n > 0 {
		// Deliberately not translated. It granted access to every project whose
		// visibility was "private", and that visibility no longer exists —
		// there is no scope this maps onto without either widening access to
		// projects that were deliberately narrower, or inventing a per-project
		// list the file never asked for. Projects that were private already had
		// this translated once, at the database level, by MigrateAccessModel.
		c.logger.Warn("config: access.private is retired and is NOT being applied; its members were migrated once into the \"Private Access\" group. Grant that group on an org or project under access.grants",
			"entries", n)
	}
}

// declaredGroups collects the groups config asks for, including those implied
// by the retired auth.*.project_groups entries.
func declaredGroups(cfg *config.Config) []config.AccessGroupConfig {
	groups := append([]config.AccessGroupConfig(nil), cfg.Access.Groups...)

	// A retired project_groups entry names one auth-provider group and one
	// project. The group half becomes an access group named after the
	// identifier, matching what MigrateAccessModel called it, so an operator
	// who upgraded and one who started fresh end up with the same names.
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.Name] = true
	}
	add := func(name string, member config.AccessGroupMemberConfig) {
		if seen[name] {
			return
		}
		seen[name] = true
		groups = append(groups, config.AccessGroupConfig{
			Name:        name,
			Description: "Imported from an auth group mapping.",
			Members:     []config.AccessGroupMemberConfig{member},
		})
	}
	for _, m := range cfg.Auth.LDAP.ProjectGroups {
		add(m.Group, config.AccessGroupMemberConfig{LDAPGroup: m.Group})
	}
	for _, m := range cfg.Auth.OAuth2.ProjectGroups {
		add(m.Group+" (oauth2)", config.AccessGroupMemberConfig{OAuth2Group: m.Group})
	}
	return groups
}

// declaredGrants collects the grants config asks for, including those implied
// by the retired auth.*.project_groups entries.
func declaredGrants(cfg *config.Config) []config.AccessGrantConfig {
	grants := append([]config.AccessGrantConfig(nil), cfg.Access.Grants...)
	for _, m := range cfg.Auth.LDAP.ProjectGroups {
		grants = append(grants, config.AccessGrantConfig{Group: m.Group, Project: m.Project, Role: m.Role})
	}
	for _, m := range cfg.Auth.OAuth2.ProjectGroups {
		grants = append(grants, config.AccessGrantConfig{Group: m.Group + " (oauth2)", Project: m.Project, Role: m.Role})
	}
	return grants
}

// syncGroups creates the declared groups and reconciles the members config
// owns, leaving members added in the UI alone.
func (c *ConfigSync) syncGroups(ctx context.Context, declared []config.AccessGroupConfig) error {
	type memberKey struct {
		groupID     int64
		subjectType string
		identifier  string
	}

	wanted := map[memberKey]bool{}

	for _, g := range declared {
		if g.Name == "" {
			c.logger.Warn("config: skipping an access group with no name")
			continue
		}

		group, err := c.groups.GetByName(ctx, g.Name)
		if err != nil || group == nil {
			group = &database.AccessGroup{Name: g.Name, Description: g.Description}
			if err := c.groups.Create(ctx, group); err != nil {
				c.logger.Error("config: creating access group", "name", g.Name, "error", err)
				continue
			}
			c.logger.Info("config: created access group", "name", g.Name)
		}

		for _, m := range g.Members {
			subjectType, identifier := memberSubject(m)
			if identifier == "" {
				c.logger.Warn("config: skipping an access group member naming nothing", "group", g.Name)
				continue
			}
			wanted[memberKey{group.ID, subjectType, identifier}] = true

			if err := c.groups.AddMember(ctx, &database.AccessGroupMember{
				GroupID:           group.ID,
				SubjectType:       subjectType,
				SubjectIdentifier: identifier,
				Source:            database.GrantSourceConfig,
			}); err != nil {
				// A duplicate is the normal steady state: the row is already
				// there from a previous startup.
				c.logger.Debug("config: access group member not added", "group", g.Name, "member", identifier, "error", err)
			}
		}
	}

	// Anything config wrote that it no longer asks for goes.
	existing, err := c.groups.ListMembersBySource(ctx, database.GrantSourceConfig)
	if err != nil {
		return fmt.Errorf("listing config-owned group members: %w", err)
	}
	for _, m := range existing {
		if wanted[memberKey{m.GroupID, m.SubjectType, m.SubjectIdentifier}] {
			continue
		}
		c.logger.Info("config: removing access group member no longer declared",
			"group_id", m.GroupID, "member", m.SubjectIdentifier)
		if err := c.groups.RemoveMember(ctx, m.ID); err != nil {
			c.logger.Error("config: removing access group member", "member_id", m.ID, "error", err)
		}
	}
	return nil
}

// memberSubject maps one declared member onto a subject type and identifier.
// Exactly one field is expected to be set; the first that is wins.
func memberSubject(m config.AccessGroupMemberConfig) (subjectType, identifier string) {
	switch {
	case m.User != "":
		return database.SubjectTypeUser, m.User
	case m.LDAPGroup != "":
		return database.SubjectTypeLDAPGroup, m.LDAPGroup
	case m.OAuth2Group != "":
		return database.SubjectTypeOAuth2Group, m.OAuth2Group
	}
	return "", ""
}

// syncGrants applies the declared grants and removes config-owned grants that
// are no longer declared.
func (c *ConfigSync) syncGrants(ctx context.Context, declared []config.AccessGrantConfig) error {
	// Resolving names to ids can fail per entry (a project renamed, a group
	// misspelled), so build the set of what actually landed rather than what
	// was asked for — otherwise a typo would look like "declared" and survive
	// the removal pass below.
	applied := map[int64]bool{}

	for _, g := range declared {
		grant, err := c.resolveGrant(ctx, g)
		if err != nil {
			c.logger.Error("config: skipping access grant", "error", err)
			continue
		}
		if err := c.grants.Grant(ctx, grant); err != nil {
			c.logger.Error("config: applying access grant", "error", err)
			continue
		}
		applied[grant.ID] = true
	}

	for _, scope := range []struct {
		list func(context.Context, int64) ([]database.AccessGrant, error)
		ids  func(context.Context) ([]int64, error)
	}{
		{c.grants.ListByOrg, c.orgIDs},
		{c.grants.ListByProject, c.projectIDs},
	} {
		ids, err := scope.ids(ctx)
		if err != nil {
			return err
		}
		for _, id := range ids {
			grants, err := scope.list(ctx, id)
			if err != nil {
				c.logger.Error("config: listing grants for reconciliation", "scope_id", id, "error", err)
				continue
			}
			for _, existing := range grants {
				if existing.Source != database.GrantSourceConfig || applied[existing.ID] {
					continue
				}
				c.logger.Info("config: revoking access grant no longer declared", "grant_id", existing.ID)
				if _, err := c.grants.Revoke(ctx, existing.ID); err != nil {
					c.logger.Error("config: revoking access grant", "grant_id", existing.ID, "error", err)
				}
			}
		}
	}
	return nil
}

// resolveGrant turns names from the config file into the ids the grant edge
// stores, refusing anything that names more or less than one subject and one
// scope.
func (c *ConfigSync) resolveGrant(ctx context.Context, g config.AccessGrantConfig) (*database.AccessGrant, error) {
	grant := &database.AccessGrant{Role: g.Role, Source: database.GrantSourceConfig}

	switch {
	case g.Group != "" && g.User != "":
		return nil, fmt.Errorf("grant names both a group (%q) and a user (%q); it must name one", g.Group, g.User)
	case g.Group != "":
		group, err := c.groups.GetByName(ctx, g.Group)
		if err != nil || group == nil {
			return nil, fmt.Errorf("grant names unknown access group %q", g.Group)
		}
		grant.GroupID = &group.ID
	case g.User != "":
		user, err := c.users.GetByUsername(ctx, g.User)
		if err != nil || user == nil {
			return nil, fmt.Errorf("grant names unknown user %q", g.User)
		}
		grant.UserID = &user.ID
	default:
		return nil, fmt.Errorf("grant names neither a group nor a user")
	}

	switch {
	case g.Org != "" && g.Project != "":
		return nil, fmt.Errorf("grant names both an org (%q) and a project (%q); it must name one", g.Org, g.Project)
	case g.Org != "":
		org, err := c.orgs.GetBySlug(ctx, g.Org)
		if err != nil || org == nil {
			return nil, fmt.Errorf("grant names unknown org %q", g.Org)
		}
		grant.OrgID = &org.ID
	case g.Project != "":
		project, err := c.projects.GetBySlug(ctx, g.Project)
		if err != nil || project == nil {
			return nil, fmt.Errorf("grant names unknown project %q", g.Project)
		}
		grant.ProjectID = &project.ID
	default:
		return nil, fmt.Errorf("grant names neither an org nor a project")
	}

	if !database.ValidGrantRole(grant.Role) {
		return nil, fmt.Errorf("grant has unknown role %q", grant.Role)
	}
	return grant, nil
}

func (c *ConfigSync) orgIDs(ctx context.Context) ([]int64, error) {
	orgs, err := c.orgs.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing orgs: %w", err)
	}
	ids := make([]int64, 0, len(orgs))
	for _, o := range orgs {
		ids = append(ids, o.ID)
	}
	return ids, nil
}

func (c *ConfigSync) projectIDs(ctx context.Context) ([]int64, error) {
	projects, err := c.projects.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	ids := make([]int64, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	return ids, nil
}

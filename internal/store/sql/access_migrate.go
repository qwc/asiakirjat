package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

// accessMigrationKey marks the one-shot translation of the old access model
// into the new one as done.
//
// It runs once and never again, deliberately. Re-running it on every startup
// would recreate grants an admin had since revoked — the access equivalent of
// a write path that undoes the operator's work behind their back.
const accessMigrationKey = "access_model_migrated"

// MigrateAccessModel translates global_access, access_lists,
// auth_group_mappings and project_access into access_groups and access_grants
// (issues #150, #151).
//
// The whole thing runs in one transaction: a half-migrated access model is
// worse than an unmigrated one, because it looks finished.
//
// The translation is intended to preserve exactly who can reach what. Where
// the old data is ambiguous it prefers the narrower reading — see
// migrateProjectAccess, which is the only place the old model does not map
// cleanly.
func MigrateAccessModel(ctx context.Context, db *sqlx.DB, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	done, err := metaFlag(ctx, db, accessMigrationKey)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning access migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	m := &migrator{tx: tx, logger: logger, groups: map[string]int64{}}
	if err := m.run(ctx); err != nil {
		return fmt.Errorf("migrating access model: %w", err)
	}

	if _, err := tx.ExecContext(ctx, tx.Rebind(
		`INSERT INTO app_meta (meta_key, meta_value) VALUES (?, ?)`), accessMigrationKey, "1"); err != nil {
		return fmt.Errorf("recording access migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing access migration: %w", err)
	}

	logger.Info("access model migrated",
		"groups", m.groupsCreated, "grants", m.grantsCreated,
		"resolved_memberships", m.resolvedCreated, "ambiguous_grants_pinned", m.ambiguousPinned)
	return nil
}

func metaFlag(ctx context.Context, db *sqlx.DB, key string) (bool, error) {
	var value string
	err := db.GetContext(ctx, &value, db.Rebind(`SELECT meta_value FROM app_meta WHERE meta_key = ?`), key)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading app_meta %q: %w", key, err)
	}
	return value == "1", nil
}

type migrator struct {
	tx     *sqlx.Tx
	logger *slog.Logger

	// groups maps a stable key to the id of the access group standing in for
	// it, so the same subject set is never created twice.
	groups map[string]int64

	groupsCreated   int
	grantsCreated   int
	resolvedCreated int
	ambiguousPinned int
}

type oldProject struct {
	ID           int64  `db:"id"`
	Slug         string `db:"slug"`
	Visibility   string `db:"visibility"`
	AccessListID *int64 `db:"access_list_id"`
	CreatedBy    *int64 `db:"created_by"`
}

func (m *migrator) run(ctx context.Context) error {
	var projects []oldProject
	if err := m.tx.SelectContext(ctx, &projects,
		`SELECT id, slug, visibility, access_list_id, created_by FROM projects`); err != nil {
		return fmt.Errorf("loading projects: %w", err)
	}

	if err := m.migrateAccessLists(ctx, projects); err != nil {
		return err
	}
	if err := m.migrateGlobalAccess(ctx, projects); err != nil {
		return err
	}
	mappingsByProject, err := m.migrateGroupMappings(ctx)
	if err != nil {
		return err
	}
	if err := m.migrateProjectAccess(ctx, mappingsByProject); err != nil {
		return err
	}
	return m.migrateCreators(ctx, projects)
}

// ensureGroup returns the id of the group standing in for key, creating it on
// first use. name is only used at creation; key is what deduplicates.
func (m *migrator) ensureGroup(ctx context.Context, key, name, description string) (int64, error) {
	if id, ok := m.groups[key]; ok {
		return id, nil
	}

	// A group of this name may already exist from an earlier step (an access
	// list and an LDAP mapping can name the same team). Reuse it: merging two
	// descriptions of the same subject set is right, and inventing "name (2)"
	// would leave the admin two identical groups to reconcile.
	var existing int64
	err := m.tx.GetContext(ctx, &existing, m.tx.Rebind(`SELECT id FROM access_groups WHERE name = ?`), name)
	if err == nil {
		m.groups[key] = existing
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("looking up access group %q: %w", name, err)
	}

	result, err := m.tx.ExecContext(ctx, m.tx.Rebind(
		`INSERT INTO access_groups (name, description) VALUES (?, ?)`), name, description)
	if err != nil {
		return 0, fmt.Errorf("creating access group %q: %w", name, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting access group id: %w", err)
	}
	m.groups[key] = id
	m.groupsCreated++
	return id, nil
}

func (m *migrator) addMember(ctx context.Context, groupID int64, subjectType, identifier string) error {
	if !database.ValidSubjectType(subjectType) {
		m.logger.Warn("access migration: skipping member with unknown subject type",
			"subject_type", subjectType, "identifier", identifier)
		return nil
	}
	var exists int
	if err := m.tx.GetContext(ctx, &exists, m.tx.Rebind(
		`SELECT COUNT(*) FROM access_group_members WHERE group_id = ? AND subject_type = ? AND subject_identifier = ?`),
		groupID, subjectType, identifier); err != nil {
		return fmt.Errorf("checking access group member: %w", err)
	}
	if exists > 0 {
		return nil
	}
	if _, err := m.tx.ExecContext(ctx, m.tx.Rebind(
		`INSERT INTO access_group_members (group_id, subject_type, subject_identifier) VALUES (?, ?, ?)`),
		groupID, subjectType, identifier); err != nil {
		return fmt.Errorf("adding access group member: %w", err)
	}
	return nil
}

// grantGroup points a group at a project. Repeat grants keep the stronger role
// rather than the last one written, so the order steps run in cannot quietly
// downgrade access.
func (m *migrator) grantGroup(ctx context.Context, groupID, projectID int64, role string) error {
	return m.grant(ctx, &groupID, nil, projectID, role)
}

func (m *migrator) grantUser(ctx context.Context, userID, projectID int64, role string) error {
	return m.grant(ctx, nil, &userID, projectID, role)
}

func (m *migrator) grant(ctx context.Context, groupID, userID *int64, projectID int64, role string) error {
	if !database.ValidGrantRole(role) {
		role = database.GrantRoleViewer
	}

	var (
		existingID   int64
		existingRole string
	)
	var err error
	if groupID != nil {
		err = m.tx.QueryRowxContext(ctx, m.tx.Rebind(
			`SELECT id, role FROM access_grants WHERE group_id = ? AND project_id = ?`),
			*groupID, projectID).Scan(&existingID, &existingRole)
	} else {
		err = m.tx.QueryRowxContext(ctx, m.tx.Rebind(
			`SELECT id, role FROM access_grants WHERE user_id = ? AND project_id = ?`),
			*userID, projectID).Scan(&existingID, &existingRole)
	}

	switch {
	case err == nil:
		if database.GrantRoleRank(role) > database.GrantRoleRank(existingRole) {
			if _, err := m.tx.ExecContext(ctx, m.tx.Rebind(
				`UPDATE access_grants SET role = ? WHERE id = ?`), role, existingID); err != nil {
				return fmt.Errorf("strengthening access grant: %w", err)
			}
		}
		return nil
	case errors.Is(err, sql.ErrNoRows):
	default:
		return fmt.Errorf("looking up access grant: %w", err)
	}

	if _, err := m.tx.ExecContext(ctx, m.tx.Rebind(
		`INSERT INTO access_grants (group_id, user_id, project_id, role, source) VALUES (?, ?, ?, ?, ?)`),
		groupID, userID, projectID, role, database.GrantSourceManual); err != nil {
		return fmt.Errorf("creating access grant: %w", err)
	}
	m.grantsCreated++
	return nil
}

func (m *migrator) addResolved(ctx context.Context, groupID, userID int64, source string) error {
	var exists int
	if err := m.tx.GetContext(ctx, &exists, m.tx.Rebind(
		`SELECT COUNT(*) FROM access_group_resolved WHERE group_id = ? AND user_id = ? AND source = ?`),
		groupID, userID, source); err != nil {
		return fmt.Errorf("checking resolved membership: %w", err)
	}
	if exists > 0 {
		return nil
	}
	if _, err := m.tx.ExecContext(ctx, m.tx.Rebind(
		`INSERT INTO access_group_resolved (group_id, user_id, source) VALUES (?, ?, ?)`),
		groupID, userID, source); err != nil {
		return fmt.Errorf("recording resolved membership: %w", err)
	}
	m.resolvedCreated++
	return nil
}

// roleSuffixedName keeps one group per (subject set, role) where the old model
// stored a role on the membership. A list whose members were all viewers keeps
// its name; one with both roles splits, because a single group cannot carry two
// roles for the same project.
func roleSuffixedName(name, role string, roles map[string]bool) string {
	if len(roles) <= 1 {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, role)
}

// migrateAccessLists turns each access list into one group per distinct member
// role, and grants those groups on the projects that pointed at the list.
func (m *migrator) migrateAccessLists(ctx context.Context, projects []oldProject) error {
	type list struct {
		ID          int64  `db:"id"`
		Name        string `db:"name"`
		Description string `db:"description"`
	}
	var lists []list
	if err := m.tx.SelectContext(ctx, &lists, `SELECT id, name, description FROM access_lists`); err != nil {
		return fmt.Errorf("loading access lists: %w", err)
	}

	type member struct {
		SubjectType       string `db:"subject_type"`
		SubjectIdentifier string `db:"subject_identifier"`
		Role              string `db:"role"`
	}

	// listRoleGroup maps (list id, role) to the group that now represents it.
	listRoleGroup := map[int64]map[string]int64{}

	for _, l := range lists {
		var members []member
		if err := m.tx.SelectContext(ctx, &members, m.tx.Rebind(
			`SELECT subject_type, subject_identifier, role FROM access_list_members WHERE list_id = ?`), l.ID); err != nil {
			return fmt.Errorf("loading access list members: %w", err)
		}

		roles := map[string]bool{}
		for _, mem := range members {
			roles[mem.Role] = true
		}

		listRoleGroup[l.ID] = map[string]int64{}
		for _, mem := range members {
			name := roleSuffixedName(l.Name, mem.Role, roles)
			groupID, err := m.ensureGroup(ctx, fmt.Sprintf("list:%d:%s", l.ID, mem.Role), name, l.Description)
			if err != nil {
				return err
			}
			listRoleGroup[l.ID][mem.Role] = groupID
			if err := m.addMember(ctx, groupID, mem.SubjectType, mem.SubjectIdentifier); err != nil {
				return err
			}
		}

		// Resolved group memberships carry the role the member had, so they
		// belong to the group that role produced.
		type resolved struct {
			UserID int64  `db:"user_id"`
			Role   string `db:"role"`
			Source string `db:"source"`
		}
		var grants []resolved
		if err := m.tx.SelectContext(ctx, &grants, m.tx.Rebind(
			`SELECT user_id, role, source FROM access_list_grants WHERE list_id = ?`), l.ID); err != nil {
			return fmt.Errorf("loading access list grants: %w", err)
		}
		for _, g := range grants {
			groupID, ok := listRoleGroup[l.ID][g.Role]
			if !ok {
				// The rule that produced this grant is gone; the strongest
				// surviving group for the list is the closest honest match.
				for _, id := range listRoleGroup[l.ID] {
					groupID, ok = id, true
					break
				}
			}
			if !ok {
				continue
			}
			if err := m.addResolved(ctx, groupID, g.UserID, g.Source); err != nil {
				return err
			}
		}
	}

	for _, p := range projects {
		if p.Visibility != database.VisibilityList || p.AccessListID == nil {
			continue
		}
		for role, groupID := range listRoleGroup[*p.AccessListID] {
			if err := m.grantGroup(ctx, groupID, p.ID, role); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateGlobalAccess turns the instance-wide private access rules into a
// group, granted on every project that was private.
//
// It is not granted on the default org, which would have been tidier: an org
// grant cascades to every project in the org, including the ones whose
// visibility was 'custom' precisely to keep those people out.
func (m *migrator) migrateGlobalAccess(ctx context.Context, projects []oldProject) error {
	type rule struct {
		SubjectType       string `db:"subject_type"`
		SubjectIdentifier string `db:"subject_identifier"`
		Role              string `db:"role"`
	}
	var rules []rule
	if err := m.tx.SelectContext(ctx, &rules,
		`SELECT subject_type, subject_identifier, role FROM global_access`); err != nil {
		return fmt.Errorf("loading global access rules: %w", err)
	}

	type grant struct {
		UserID int64  `db:"user_id"`
		Role   string `db:"role"`
		Source string `db:"source"`
	}
	var grants []grant
	if err := m.tx.SelectContext(ctx, &grants,
		`SELECT user_id, role, source FROM global_access_grants`); err != nil {
		return fmt.Errorf("loading global access grants: %w", err)
	}

	if len(rules) == 0 && len(grants) == 0 {
		return nil
	}

	roles := map[string]bool{}
	for _, r := range rules {
		roles[r.Role] = true
	}
	for _, g := range grants {
		roles[g.Role] = true
	}

	roleGroup := map[string]int64{}
	ensure := func(role string) (int64, error) {
		if id, ok := roleGroup[role]; ok {
			return id, nil
		}
		name := roleSuffixedName("Private Access", role, roles)
		id, err := m.ensureGroup(ctx, "global:"+role, name,
			"Everyone who could reach private projects before organizations existed.")
		if err != nil {
			return 0, err
		}
		roleGroup[role] = id
		return id, nil
	}

	for _, r := range rules {
		groupID, err := ensure(r.Role)
		if err != nil {
			return err
		}
		if err := m.addMember(ctx, groupID, r.SubjectType, r.SubjectIdentifier); err != nil {
			return err
		}
	}
	for _, g := range grants {
		groupID, err := ensure(g.Role)
		if err != nil {
			return err
		}
		// A 'manual' global grant was an admin naming a user directly, with no
		// rule behind it. It has no auth source to re-resolve from, so it
		// becomes an ordinary user member instead of a resolved membership,
		// which would evaporate at that user's next login.
		if g.Source == database.AccessSourceManual {
			var username string
			if err := m.tx.GetContext(ctx, &username, m.tx.Rebind(
				`SELECT username FROM users WHERE id = ?`), g.UserID); err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("looking up user for global grant: %w", err)
				}
				continue
			}
			if err := m.addMember(ctx, groupID, database.SubjectTypeUser, username); err != nil {
				return err
			}
			continue
		}
		if err := m.addResolved(ctx, groupID, g.UserID, g.Source); err != nil {
			return err
		}
	}

	for _, p := range projects {
		if p.Visibility != database.VisibilityPrivate {
			continue
		}
		for role, groupID := range roleGroup {
			if err := m.grantGroup(ctx, groupID, p.ID, role); err != nil {
				return err
			}
		}
	}
	return nil
}

// mappingInfo remembers what an auth group mapping became, so project_access
// rows synced from it can be traced back.
type mappingInfo struct {
	GroupID int64
	Source  string
	Role    string
}

// migrateGroupMappings turns each auth group mapping into a group holding that
// one auth-provider group, granted on the mapped project. Two mappings of the
// same group to different projects with different roles now express what the
// old model could not.
func (m *migrator) migrateGroupMappings(ctx context.Context) (map[int64][]mappingInfo, error) {
	type mapping struct {
		AuthSource      string `db:"auth_source"`
		GroupIdentifier string `db:"group_identifier"`
		ProjectID       int64  `db:"project_id"`
		Role            string `db:"role"`
	}
	var mappings []mapping
	if err := m.tx.SelectContext(ctx, &mappings,
		`SELECT auth_source, group_identifier, project_id, role FROM auth_group_mappings`); err != nil {
		return nil, fmt.Errorf("loading auth group mappings: %w", err)
	}

	byProject := map[int64][]mappingInfo{}
	for _, mp := range mappings {
		subjectType := database.SubjectTypeLDAPGroup
		if mp.AuthSource == "oauth2" {
			subjectType = database.SubjectTypeOAuth2Group
		}

		// Key on the subject, not the name: an LDAP group and an OAuth2 group
		// may share an identifier while being entirely different sets of
		// people, and merging them would widen access.
		key := subjectType + ":" + mp.GroupIdentifier
		name := mp.GroupIdentifier
		if subjectType == database.SubjectTypeOAuth2Group {
			name = mp.GroupIdentifier + " (oauth2)"
		}
		groupID, err := m.ensureGroup(ctx, key, name, "Imported from an auth group mapping.")
		if err != nil {
			return nil, err
		}
		if err := m.addMember(ctx, groupID, subjectType, mp.GroupIdentifier); err != nil {
			return nil, err
		}
		if err := m.grantGroup(ctx, groupID, mp.ProjectID, mp.Role); err != nil {
			return nil, err
		}
		byProject[mp.ProjectID] = append(byProject[mp.ProjectID], mappingInfo{
			GroupID: groupID, Source: mp.AuthSource, Role: mp.Role,
		})
	}
	return byProject, nil
}

// migrateProjectAccess is the only place the old model does not map cleanly.
//
//   - A 'manual' row is an admin naming a user on a project, which is exactly
//     a direct user grant.
//   - An 'ldap'/'oauth2' row was written by the login sync from a group
//     mapping. The mapping itself is already a group grant, so what is missing
//     is the fact that this user was in that group. Where exactly one mapping
//     of that source targets the project, that fact is recoverable and becomes
//     a resolved membership, keeping the sync's ability to revoke it later.
//   - Where several mappings of the same source target the project, it is not
//     recoverable: the user was in at least one of those groups, and assuming
//     all of them would grant access to everything those groups reach
//     elsewhere. Those become a direct user grant on this project only —
//     exactly the access they had, never more — and are logged so an admin can
//     tidy up.
func (m *migrator) migrateProjectAccess(ctx context.Context, mappingsByProject map[int64][]mappingInfo) error {
	type row struct {
		ProjectID int64  `db:"project_id"`
		UserID    int64  `db:"user_id"`
		Role      string `db:"role"`
		Source    string `db:"source"`
	}
	var rows []row
	if err := m.tx.SelectContext(ctx, &rows,
		`SELECT project_id, user_id, role, source FROM project_access`); err != nil {
		return fmt.Errorf("loading project access: %w", err)
	}

	for _, r := range rows {
		if r.Source == database.AccessSourceManual {
			if err := m.grantUser(ctx, r.UserID, r.ProjectID, r.Role); err != nil {
				return err
			}
			continue
		}

		var candidates []mappingInfo
		for _, mi := range mappingsByProject[r.ProjectID] {
			if mi.Source == r.Source {
				candidates = append(candidates, mi)
			}
		}

		switch len(candidates) {
		case 1:
			if err := m.addResolved(ctx, candidates[0].GroupID, r.UserID, r.Source); err != nil {
				return err
			}
		default:
			m.ambiguousPinned++
			m.logger.Warn("access migration: synced access pinned as a direct grant",
				"project_id", r.ProjectID, "user_id", r.UserID, "source", r.Source,
				"candidate_mappings", len(candidates),
				"reason", "cannot tell which group mapping granted this; a direct grant preserves exactly this access and no more")
			if err := m.grantUser(ctx, r.UserID, r.ProjectID, r.Role); err != nil {
				return err
			}
		}
	}
	return nil
}

// migrateCreators turns project ownership into data. PR #118 let a non-admin
// creator manage the projects they made, decided from projects.created_by in
// the checker; the new model says the same thing with an admin grant, so the
// checker needs no ownership branch.
func (m *migrator) migrateCreators(ctx context.Context, projects []oldProject) error {
	for _, p := range projects {
		if p.CreatedBy == nil {
			continue
		}
		var exists int
		if err := m.tx.GetContext(ctx, &exists, m.tx.Rebind(
			`SELECT COUNT(*) FROM users WHERE id = ?`), *p.CreatedBy); err != nil {
			return fmt.Errorf("checking project creator: %w", err)
		}
		if exists == 0 {
			continue
		}
		if err := m.grantUser(ctx, *p.CreatedBy, p.ID, database.GrantRoleAdmin); err != nil {
			return err
		}
	}
	return nil
}

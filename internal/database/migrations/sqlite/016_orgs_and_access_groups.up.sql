-- One access model to replace four (issues #150, #151).
--
-- Before this, four mechanisms granted access with three copies of the same
-- shape: global_access rules, access_list_members and auth_group_mappings all
-- store (subject_type, subject_identifier, role), and global_access_grants,
-- access_list_grants and project_access are three parallel resolved-grant
-- tables. Each arrived with a feature; none of them subsume the others, so a
-- project's real permissions were spread over all four.
--
-- The replacement is one noun and one edge:
--
--   access_groups          a named set of subjects (users and/or auth groups)
--   access_grants          group-or-user -> org-or-project, with a role
--
-- The role lives on the GRANT, not on the membership. That is the whole point
-- of the change: access_list_members.role forced a list to carry one role
-- everywhere, so "engineering edits A but only reads B" needed two lists.
--
-- This migration only creates the new shape and backfills orgs. Existing
-- access is translated by the one-shot migration in internal/access/migrate.go,
-- and nothing reads the new tables until the checker is switched over.

CREATE TABLE IF NOT EXISTS orgs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS access_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Deliberately has no role column; see the header.
CREATE TABLE IF NOT EXISTS access_group_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES access_groups(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL,        -- 'user', 'ldap_group', 'oauth2_group'
    subject_identifier TEXT NOT NULL,  -- username, LDAP DN, OAuth2 group name
    UNIQUE(group_id, subject_type, subject_identifier)
);

-- Group membership resolved for one user. A member naming a user is matched by
-- username when access is checked and needs no row here; a member naming an
-- LDAP or OAuth2 group can only be resolved while that user signs in, so the
-- login sync records the outcome. Same split as before, one table instead of
-- three.
CREATE TABLE IF NOT EXISTS access_group_resolved (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES access_groups(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source TEXT NOT NULL,              -- 'ldap' or 'oauth2'
    UNIQUE(group_id, user_id, source)
);

-- The grant edge. Exactly one subject column and exactly one scope column are
-- set; the CHECK constraints enforce that rather than trusting callers.
--
-- Real foreign keys on all four columns are the reason this is not one
-- polymorphic (subject_type, subject_id) pair: a grant whose project has been
-- deleted must disappear with it. An orphan row that later matched a reused id
-- would silently grant access to a different project.
CREATE TABLE IF NOT EXISTS access_grants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id   INTEGER REFERENCES access_groups(id) ON DELETE CASCADE,
    user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
    org_id     INTEGER REFERENCES orgs(id) ON DELETE CASCADE,
    project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
    role TEXT NOT NULL,                     -- 'viewer', 'editor', 'admin'
    source TEXT NOT NULL DEFAULT 'manual',  -- 'manual' or 'config'
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((group_id IS NOT NULL) <> (user_id IS NOT NULL)),
    CHECK ((org_id IS NOT NULL) <> (project_id IS NOT NULL))
);

-- NULLs compare distinct, so each index only constrains the rows that actually
-- use that subject/scope pairing.
CREATE UNIQUE INDEX idx_access_grants_group_project ON access_grants(group_id, project_id);
CREATE UNIQUE INDEX idx_access_grants_user_project  ON access_grants(user_id, project_id);
CREATE UNIQUE INDEX idx_access_grants_group_org     ON access_grants(group_id, org_id);
CREATE UNIQUE INDEX idx_access_grants_user_org      ON access_grants(user_id, org_id);
CREATE INDEX idx_access_grants_project ON access_grants(project_id);
CREATE INDEX idx_access_grants_org ON access_grants(org_id);
CREATE INDEX idx_access_group_members_group ON access_group_members(group_id);
CREATE INDEX idx_access_group_resolved_user ON access_group_resolved(user_id);

-- Every project belongs to exactly one org. Existing installations have none,
-- so they get a default one; it is an ordinary org that happens to hold
-- everything that predates the feature. Orgs never appear in URLs, so its slug
-- cannot collide with a project's.
INSERT INTO orgs (slug, name, description)
    VALUES ('default', 'No Org', 'Projects that predate organizations.');

ALTER TABLE projects ADD COLUMN org_id INTEGER REFERENCES orgs(id);
UPDATE projects SET org_id = (SELECT id FROM orgs WHERE slug = 'default');
CREATE INDEX idx_projects_org ON projects(org_id);

-- Exposure replaces the four visibility values. public and custom/private/list
-- differed in *who* could reach a project, which is now entirely a question of
-- grants; the only thing left for the project itself to say is how far it
-- reaches beyond them:
--
--   public         anyone, including signed-out visitors
--   authenticated  any signed-in user
--   granted        only what access_grants allows
--
-- visibility stays for now so the old checker keeps working; it is dropped once
-- nothing reads it.
ALTER TABLE projects ADD COLUMN exposure TEXT NOT NULL DEFAULT 'granted';
UPDATE projects SET exposure = 'public' WHERE visibility = 'public';

-- Marker table for one-shot data migrations that are too conditional for SQL.
CREATE TABLE IF NOT EXISTS app_meta (
    meta_key TEXT PRIMARY KEY,
    meta_value TEXT NOT NULL
);

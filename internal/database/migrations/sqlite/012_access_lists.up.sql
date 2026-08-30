-- Named access lists: a reusable set of subjects that a project's visibility
-- can point at, so access can be described once ("engineering") and shared by
-- many projects instead of repeating per-project grants (issue #125).
--
-- Membership deliberately mirrors global_access rules: a list can be a single
-- LDAP group, or an LDAP group plus a handful of named users.
CREATE TABLE IF NOT EXISTS access_lists (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS access_list_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    list_id INTEGER NOT NULL REFERENCES access_lists(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL,           -- 'user', 'ldap_group', 'oauth2_group'
    subject_identifier TEXT NOT NULL,     -- username, LDAP DN, OAuth2 group name
    role TEXT NOT NULL DEFAULT 'viewer',  -- 'viewer' or 'editor'
    UNIQUE(list_id, subject_type, subject_identifier)
);

-- Set when visibility = 'list'. ON DELETE RESTRICT: a list still chosen by a
-- project cannot be deleted out from under it, which would silently change who
-- can reach that project.
ALTER TABLE projects ADD COLUMN access_list_id INTEGER REFERENCES access_lists(id) ON DELETE RESTRICT;

CREATE INDEX idx_projects_access_list ON projects(access_list_id);
CREATE INDEX idx_access_list_members_list ON access_list_members(list_id);

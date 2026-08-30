-- Named access lists: a reusable set of subjects that a project's visibility
-- can point at (issue #125). Membership mirrors global_access rules, so a list
-- can be a single LDAP group, or an LDAP group plus named users.
CREATE TABLE IF NOT EXISTS access_lists (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS access_list_members (
    id BIGSERIAL PRIMARY KEY,
    list_id BIGINT NOT NULL REFERENCES access_lists(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL,
    subject_identifier TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'viewer',
    UNIQUE(list_id, subject_type, subject_identifier)
);

ALTER TABLE projects ADD COLUMN access_list_id BIGINT REFERENCES access_lists(id) ON DELETE RESTRICT;

CREATE INDEX idx_projects_access_list ON projects(access_list_id);
CREATE INDEX idx_access_list_members_list ON access_list_members(list_id);

-- One access model to replace four (issues #150, #151). See the sqlite copy of
-- this migration for the reasoning; the schema is the same, in Postgres types.

CREATE TABLE IF NOT EXISTS orgs (
    id BIGSERIAL PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS access_groups (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Deliberately has no role column: the role lives on the grant.
CREATE TABLE IF NOT EXISTS access_group_members (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL REFERENCES access_groups(id) ON DELETE CASCADE,
    subject_type TEXT NOT NULL,
    subject_identifier TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'manual',
    UNIQUE(group_id, subject_type, subject_identifier)
);

CREATE TABLE IF NOT EXISTS access_group_resolved (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL REFERENCES access_groups(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    UNIQUE(group_id, user_id, source)
);

CREATE TABLE IF NOT EXISTS access_grants (
    id BIGSERIAL PRIMARY KEY,
    group_id   BIGINT REFERENCES access_groups(id) ON DELETE CASCADE,
    user_id    BIGINT REFERENCES users(id) ON DELETE CASCADE,
    org_id     BIGINT REFERENCES orgs(id) ON DELETE CASCADE,
    project_id BIGINT REFERENCES projects(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'manual',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK ((group_id IS NOT NULL) <> (user_id IS NOT NULL)),
    CHECK ((org_id IS NOT NULL) <> (project_id IS NOT NULL))
);

CREATE UNIQUE INDEX idx_access_grants_group_project ON access_grants(group_id, project_id);
CREATE UNIQUE INDEX idx_access_grants_user_project  ON access_grants(user_id, project_id);
CREATE UNIQUE INDEX idx_access_grants_group_org     ON access_grants(group_id, org_id);
CREATE UNIQUE INDEX idx_access_grants_user_org      ON access_grants(user_id, org_id);
CREATE INDEX idx_access_grants_project ON access_grants(project_id);
CREATE INDEX idx_access_grants_org ON access_grants(org_id);
CREATE INDEX idx_access_group_members_group ON access_group_members(group_id);
CREATE INDEX idx_access_group_resolved_user ON access_group_resolved(user_id);

INSERT INTO orgs (slug, name, description)
    VALUES ('default', 'No Org', 'Projects that predate organizations.');

ALTER TABLE projects ADD COLUMN org_id BIGINT REFERENCES orgs(id);
UPDATE projects SET org_id = (SELECT id FROM orgs WHERE slug = 'default');
CREATE INDEX idx_projects_org ON projects(org_id);

ALTER TABLE projects ADD COLUMN exposure TEXT NOT NULL DEFAULT 'granted';
UPDATE projects SET exposure = 'public' WHERE visibility = 'public';

CREATE TABLE IF NOT EXISTS app_meta (
    meta_key TEXT PRIMARY KEY,
    meta_value TEXT NOT NULL
);

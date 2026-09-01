-- One access model to replace four (issues #150, #151). See the sqlite copy of
-- this migration for the reasoning; the schema is the same, in MySQL types.
--
-- CHECK constraints are enforced from MySQL 8.0.16; on older servers they are
-- parsed and ignored, so the stores validate the same invariants too.

CREATE TABLE IF NOT EXISTS orgs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    slug VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_orgs_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS access_groups (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_access_groups_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Deliberately has no role column: the role lives on the grant.
CREATE TABLE IF NOT EXISTS access_group_members (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    group_id BIGINT NOT NULL,
    subject_type VARCHAR(50) NOT NULL,
    subject_identifier VARCHAR(255) NOT NULL,
    UNIQUE KEY uq_group_subject (group_id, subject_type, subject_identifier),
    KEY idx_access_group_members_group (group_id),
    FOREIGN KEY (group_id) REFERENCES access_groups(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS access_group_resolved (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    group_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    source VARCHAR(50) NOT NULL,
    UNIQUE KEY uq_group_user_source (group_id, user_id, source),
    KEY idx_access_group_resolved_user (user_id),
    FOREIGN KEY (group_id) REFERENCES access_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS access_grants (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    group_id BIGINT NULL,
    user_id BIGINT NULL,
    org_id BIGINT NULL,
    project_id BIGINT NULL,
    role VARCHAR(50) NOT NULL,
    source VARCHAR(50) NOT NULL DEFAULT 'manual',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_grants_group_project (group_id, project_id),
    UNIQUE KEY uq_grants_user_project (user_id, project_id),
    UNIQUE KEY uq_grants_group_org (group_id, org_id),
    UNIQUE KEY uq_grants_user_org (user_id, org_id),
    KEY idx_access_grants_project (project_id),
    KEY idx_access_grants_org (org_id),
    FOREIGN KEY (group_id) REFERENCES access_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    CONSTRAINT chk_grants_one_subject CHECK ((group_id IS NOT NULL) <> (user_id IS NOT NULL)),
    CONSTRAINT chk_grants_one_scope CHECK ((org_id IS NOT NULL) <> (project_id IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO orgs (slug, name, description)
    VALUES ('default', 'No Org', 'Projects that predate organizations.');

ALTER TABLE projects ADD COLUMN org_id BIGINT NULL,
    ADD CONSTRAINT fk_projects_org FOREIGN KEY (org_id) REFERENCES orgs(id);
UPDATE projects SET org_id = (SELECT id FROM orgs WHERE slug = 'default');
CREATE INDEX idx_projects_org ON projects(org_id);

ALTER TABLE projects ADD COLUMN exposure VARCHAR(50) NOT NULL DEFAULT 'granted';
UPDATE projects SET exposure = 'public' WHERE visibility = 'public';

CREATE TABLE IF NOT EXISTS app_meta (
    meta_key VARCHAR(191) PRIMARY KEY,
    meta_value TEXT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

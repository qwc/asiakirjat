-- Add source column to project_access to track where access came from
ALTER TABLE project_access ADD COLUMN source VARCHAR(50) NOT NULL DEFAULT 'manual';

-- Drop old unique constraint and create new one that includes source
ALTER TABLE project_access DROP INDEX uq_project_user;
CREATE UNIQUE INDEX idx_project_access_source ON project_access(project_id, user_id, source);

-- Create auth_group_mappings table for LDAP/OAuth2 group-to-project mappings
CREATE TABLE IF NOT EXISTS auth_group_mappings (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    auth_source VARCHAR(50) NOT NULL,  -- 'ldap' or 'oauth2'
    group_identifier VARCHAR(512) NOT NULL,  -- LDAP DN or OAuth group name
    project_id BIGINT NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',
    from_config BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_auth_group_mapping (auth_source, group_identifier(255), project_id),
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE INDEX idx_auth_group_mappings_source ON auth_group_mappings(auth_source);
CREATE INDEX idx_auth_group_mappings_project ON auth_group_mappings(project_id);

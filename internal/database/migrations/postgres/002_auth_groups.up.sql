-- Add source column to project_access to track where access came from
ALTER TABLE project_access ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';

-- Drop old unique constraint and create new one that includes source
ALTER TABLE project_access DROP CONSTRAINT IF EXISTS project_access_project_id_user_id_key;
CREATE UNIQUE INDEX idx_project_access_source ON project_access(project_id, user_id, source);

-- Create auth_group_mappings table for LDAP/OAuth2 group-to-project mappings
CREATE TABLE IF NOT EXISTS auth_group_mappings (
    id BIGSERIAL PRIMARY KEY,
    auth_source TEXT NOT NULL,  -- 'ldap' or 'oauth2'
    group_identifier TEXT NOT NULL,  -- LDAP DN or OAuth group name
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'viewer',
    from_config BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(auth_source, group_identifier, project_id)
);

CREATE INDEX idx_auth_group_mappings_source ON auth_group_mappings(auth_source);
CREATE INDEX idx_auth_group_mappings_project ON auth_group_mappings(project_id);

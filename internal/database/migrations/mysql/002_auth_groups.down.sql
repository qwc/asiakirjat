-- Drop auth_group_mappings table
DROP TABLE IF EXISTS auth_group_mappings;

-- Remove source column and restore original unique constraint
DROP INDEX idx_project_access_source ON project_access;
ALTER TABLE project_access DROP COLUMN source;
CREATE UNIQUE INDEX uq_project_user ON project_access(project_id, user_id);

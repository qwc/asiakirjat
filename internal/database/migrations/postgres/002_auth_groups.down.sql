-- Drop auth_group_mappings table
DROP TABLE IF EXISTS auth_group_mappings;

-- Remove source column and restore original unique constraint
DROP INDEX IF EXISTS idx_project_access_source;
ALTER TABLE project_access DROP COLUMN IF EXISTS source;
CREATE UNIQUE INDEX project_access_project_id_user_id_key ON project_access(project_id, user_id);

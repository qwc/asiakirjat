UPDATE projects SET visibility = 'custom' WHERE visibility = 'list';

DROP INDEX IF EXISTS idx_access_list_members_list;
DROP INDEX IF EXISTS idx_projects_access_list;
ALTER TABLE projects DROP COLUMN IF EXISTS access_list_id;
DROP TABLE IF EXISTS access_list_members;
DROP TABLE IF EXISTS access_lists;

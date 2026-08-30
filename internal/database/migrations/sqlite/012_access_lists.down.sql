-- Projects pointing at a list lose that pointer; visibility is reset to
-- 'custom' so they fail closed rather than widening to a broader rule.
UPDATE projects SET visibility = 'custom' WHERE visibility = 'list';

-- The index must go first: SQLite refuses to drop an indexed column.
DROP INDEX IF EXISTS idx_access_list_members_list;
DROP INDEX IF EXISTS idx_projects_access_list;

ALTER TABLE projects DROP COLUMN access_list_id;

DROP TABLE IF EXISTS access_list_members;
DROP TABLE IF EXISTS access_lists;

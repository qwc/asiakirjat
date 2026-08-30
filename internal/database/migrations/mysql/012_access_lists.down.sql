UPDATE projects SET visibility = 'custom' WHERE visibility = 'list';

ALTER TABLE projects DROP FOREIGN KEY fk_projects_access_list;
ALTER TABLE projects DROP COLUMN access_list_id;
DROP TABLE IF EXISTS access_list_members;
DROP TABLE IF EXISTS access_lists;

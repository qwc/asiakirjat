ALTER TABLE projects DROP COLUMN exposure;
ALTER TABLE projects DROP FOREIGN KEY fk_projects_org;
ALTER TABLE projects DROP COLUMN org_id;

DROP TABLE IF EXISTS access_grants;
DROP TABLE IF EXISTS access_group_resolved;
DROP TABLE IF EXISTS access_group_members;
DROP TABLE IF EXISTS access_groups;
DROP TABLE IF EXISTS orgs;
DROP TABLE IF EXISTS app_meta;

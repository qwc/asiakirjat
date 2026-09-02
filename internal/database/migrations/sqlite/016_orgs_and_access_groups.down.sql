-- Indexes first: SQLite refuses to drop an indexed column.
DROP INDEX IF EXISTS idx_projects_org;
DROP INDEX IF EXISTS idx_access_group_resolved_user;
DROP INDEX IF EXISTS idx_access_group_members_group;
DROP INDEX IF EXISTS idx_access_grants_org;
DROP INDEX IF EXISTS idx_access_grants_project;
DROP INDEX IF EXISTS idx_access_grants_user_org;
DROP INDEX IF EXISTS idx_access_grants_group_org;
DROP INDEX IF EXISTS idx_access_grants_user_project;
DROP INDEX IF EXISTS idx_access_grants_group_project;

ALTER TABLE projects DROP COLUMN exposure;
ALTER TABLE projects DROP COLUMN org_id;

DROP TABLE IF EXISTS access_grants;
DROP TABLE IF EXISTS access_group_resolved;
DROP TABLE IF EXISTS access_group_members;
DROP TABLE IF EXISTS access_groups;
DROP TABLE IF EXISTS orgs;
DROP TABLE IF EXISTS app_meta;

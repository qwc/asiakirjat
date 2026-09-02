ALTER TABLE projects DROP COLUMN IF EXISTS exposure;
ALTER TABLE projects DROP COLUMN IF EXISTS org_id;

DROP TABLE IF EXISTS access_grants;
DROP TABLE IF EXISTS access_group_resolved;
DROP TABLE IF EXISTS access_group_members;
DROP TABLE IF EXISTS access_groups;
DROP TABLE IF EXISTS orgs;
DROP TABLE IF EXISTS app_meta;

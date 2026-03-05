DROP TABLE IF EXISTS upload_logs;
ALTER TABLE projects DROP COLUMN pinned_version;
ALTER TABLE projects DROP COLUMN pin_permanent;

-- SQLite does not support DROP COLUMN before 3.35.0; recreate table
CREATE TABLE projects_backup AS SELECT id, slug, name, description, visibility, created_at, updated_at FROM projects;
DROP TABLE projects;
ALTER TABLE projects_backup RENAME TO projects;

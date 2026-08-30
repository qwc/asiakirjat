-- Finish what 002_auth_groups started: uniqueness on project_access must
-- include source, so a user can hold a manual grant and a synced LDAP or
-- OAuth2 grant on the same project at once.
--
-- 002 tried `DROP INDEX IF EXISTS idx_project_access_unique`, but on SQLite
-- the constraint came from the table-level UNIQUE(project_id, user_id) in
-- 001_initial. Its auto-index (sqlite_autoindex_project_access_1) cannot be
-- removed with DROP INDEX, so the statement matched nothing and the old
-- constraint stayed live: any second-source Grant failed with
-- "UNIQUE constraint failed: project_access.project_id, project_access.user_id"
-- and the group mapping never applied. Postgres and MySQL dropped theirs
-- correctly, so this repair is SQLite-only (issue #133).
--
-- The table must be rebuilt because SQLite cannot drop an inline constraint.
CREATE TABLE project_access_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'viewer',
    source TEXT NOT NULL DEFAULT 'manual'
);

-- Existing rows are already unique per (project_id, user_id), which is
-- stricter than the target constraint, so every row carries over.
INSERT INTO project_access_new (id, project_id, user_id, role, source)
SELECT id, project_id, user_id, role, source FROM project_access;

DROP TABLE project_access;
ALTER TABLE project_access_new RENAME TO project_access;

CREATE UNIQUE INDEX idx_project_access_source ON project_access(project_id, user_id, source);

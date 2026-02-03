-- Drop auth_group_mappings table
DROP TABLE IF EXISTS auth_group_mappings;

-- Remove source column - SQLite doesn't support DROP COLUMN in older versions
-- We recreate the table without the source column
CREATE TABLE project_access_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'viewer',
    UNIQUE(project_id, user_id)
);

INSERT INTO project_access_new (id, project_id, user_id, role)
SELECT id, project_id, user_id, role FROM project_access
WHERE source = 'manual' OR id IN (
    SELECT MIN(id) FROM project_access GROUP BY project_id, user_id
);

DROP TABLE project_access;
ALTER TABLE project_access_new RENAME TO project_access;

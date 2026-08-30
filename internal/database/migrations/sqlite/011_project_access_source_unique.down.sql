-- Restore the inline UNIQUE(project_id, user_id) constraint. Rows that only
-- exist because several sources granted the same user are collapsed, keeping
-- the manual grant where there is one, mirroring 002_auth_groups.down.sql.
CREATE TABLE project_access_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'viewer',
    source TEXT NOT NULL DEFAULT 'manual',
    UNIQUE(project_id, user_id)
);

INSERT INTO project_access_old (id, project_id, user_id, role, source)
SELECT id, project_id, user_id, role, source FROM project_access
WHERE id IN (
    SELECT MIN(CASE WHEN source = 'manual' THEN id END)
    FROM project_access GROUP BY project_id, user_id
) OR id IN (
    SELECT MIN(id) FROM project_access
    GROUP BY project_id, user_id
    HAVING SUM(CASE WHEN source = 'manual' THEN 1 ELSE 0 END) = 0
);

DROP TABLE project_access;
ALTER TABLE project_access_old RENAME TO project_access;

CREATE UNIQUE INDEX idx_project_access_source ON project_access(project_id, user_id, source);

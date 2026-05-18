-- Reverse 009: restore NOT NULL uploaded_by and the original FK (no ON DELETE).
-- WARNING: fails if any uploaded_by is NULL (rows whose uploader has been
-- deleted under the new schema). Run a manual cleanup first if needed.

CREATE TABLE versions_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    uploaded_by INTEGER NOT NULL REFERENCES users(id),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    content_type TEXT NOT NULL DEFAULT 'archive',
    UNIQUE(project_id, tag)
);

INSERT INTO versions_old (id, project_id, tag, storage_path, uploaded_by, created_at, content_type)
SELECT id, project_id, tag, storage_path, uploaded_by, created_at, content_type FROM versions;

DROP TABLE versions;
ALTER TABLE versions_old RENAME TO versions;

CREATE TABLE upload_logs_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_tag TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'archive',
    uploaded_by INTEGER NOT NULL REFERENCES users(id),
    is_reupload BOOLEAN NOT NULL DEFAULT FALSE,
    filename TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO upload_logs_old (id, project_id, version_tag, content_type, uploaded_by, is_reupload, filename, created_at)
SELECT id, project_id, version_tag, content_type, uploaded_by, is_reupload, filename, created_at FROM upload_logs;

DROP TABLE upload_logs;
ALTER TABLE upload_logs_old RENAME TO upload_logs;

-- SQLite can't ALTER a column's constraints in place; recreate both tables
-- so uploaded_by becomes nullable and is detached on user delete instead of
-- blocking the delete.
--
-- Nothing references versions or upload_logs by FK, so no incoming-FK
-- breakage when we drop and rename.

CREATE TABLE versions_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    uploaded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    content_type TEXT NOT NULL DEFAULT 'archive',
    UNIQUE(project_id, tag)
);

INSERT INTO versions_new (id, project_id, tag, storage_path, uploaded_by, created_at, content_type)
SELECT id, project_id, tag, storage_path, uploaded_by, created_at, content_type FROM versions;

DROP TABLE versions;
ALTER TABLE versions_new RENAME TO versions;

CREATE TABLE upload_logs_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_tag TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'archive',
    uploaded_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    is_reupload BOOLEAN NOT NULL DEFAULT FALSE,
    filename TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO upload_logs_new (id, project_id, version_tag, content_type, uploaded_by, is_reupload, filename, created_at)
SELECT id, project_id, version_tag, content_type, uploaded_by, is_reupload, filename, created_at FROM upload_logs;

DROP TABLE upload_logs;
ALTER TABLE upload_logs_new RENAME TO upload_logs;

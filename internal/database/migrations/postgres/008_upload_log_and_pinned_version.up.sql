CREATE TABLE upload_logs (
    id SERIAL PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version_tag TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'archive',
    uploaded_by INTEGER NOT NULL REFERENCES users(id),
    is_reupload BOOLEAN NOT NULL DEFAULT FALSE,
    filename TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE projects ADD COLUMN pinned_version TEXT;
ALTER TABLE projects ADD COLUMN pin_permanent BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE upload_logs (
    id INTEGER PRIMARY KEY AUTO_INCREMENT,
    project_id INTEGER NOT NULL,
    version_tag TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'archive',
    uploaded_by INTEGER NOT NULL,
    is_reupload BOOLEAN NOT NULL DEFAULT FALSE,
    filename TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
    FOREIGN KEY (uploaded_by) REFERENCES users(id)
);

ALTER TABLE projects ADD COLUMN pinned_version TEXT;
ALTER TABLE projects ADD COLUMN pin_permanent BOOLEAN NOT NULL DEFAULT FALSE;

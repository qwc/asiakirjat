-- Track which user created each project, so creators (not just admins) can
-- manage their own projects. Nullable; ON DELETE SET NULL mirrors the
-- uploaded_by treatment from migration 009.
ALTER TABLE projects ADD COLUMN created_by BIGINT NULL;
ALTER TABLE projects ADD CONSTRAINT projects_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL;

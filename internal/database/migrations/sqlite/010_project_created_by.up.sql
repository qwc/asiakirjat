-- Track which user created each project, so creators (not just admins) can
-- manage their own projects. Nullable: pre-existing projects and projects
-- whose creator is later deleted have no creator. ON DELETE SET NULL mirrors
-- the uploaded_by treatment from migration 009 — removing a user must not
-- block on, or cascade-delete, the projects they created.
ALTER TABLE projects ADD COLUMN created_by INTEGER REFERENCES users(id) ON DELETE SET NULL;

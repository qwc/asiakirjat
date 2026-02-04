-- Add visibility column to projects (replaces is_public)
-- SQLite doesn't support DROP COLUMN, so we keep is_public but stop using it in code.
ALTER TABLE projects ADD COLUMN visibility TEXT NOT NULL DEFAULT 'custom';

-- Migrate existing data: is_public=1 → 'public', is_public=0 → 'custom'
UPDATE projects SET visibility = 'public' WHERE is_public = 1;
UPDATE projects SET visibility = 'custom' WHERE is_public = 0;

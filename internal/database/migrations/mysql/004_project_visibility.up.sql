-- Add visibility column to projects (replaces is_public)
ALTER TABLE projects ADD COLUMN visibility VARCHAR(50) NOT NULL DEFAULT 'custom';

-- Migrate existing data: is_public=true → 'public', is_public=false → 'custom'
UPDATE projects SET visibility = 'public' WHERE is_public = TRUE;
UPDATE projects SET visibility = 'custom' WHERE is_public = FALSE;

-- Drop the old column
ALTER TABLE projects DROP COLUMN is_public;

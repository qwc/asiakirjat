-- Re-add is_public column
ALTER TABLE projects ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT FALSE;

-- Sync data back
UPDATE projects SET is_public = TRUE WHERE visibility = 'public';
UPDATE projects SET is_public = FALSE WHERE visibility != 'public';

-- Drop visibility column
ALTER TABLE projects DROP COLUMN visibility;

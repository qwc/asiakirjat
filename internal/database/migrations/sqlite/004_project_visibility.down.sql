-- Sync is_public back from visibility before removing the column
UPDATE projects SET is_public = 1 WHERE visibility = 'public';
UPDATE projects SET is_public = 0 WHERE visibility != 'public';

-- SQLite doesn't support DROP COLUMN in older versions, but golang-migrate
-- tracks version; rolling back just needs the data synced above.
-- We can't remove the column in SQLite, but the code won't reference it.

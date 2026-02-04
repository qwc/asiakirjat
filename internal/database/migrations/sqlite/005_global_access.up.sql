-- Global access rules for "private" visibility projects.
-- Rules can come from config file (from_config=1) or admin UI.
CREATE TABLE IF NOT EXISTS global_access (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subject_type TEXT NOT NULL,           -- 'user', 'ldap_group', 'oauth2_group'
    subject_identifier TEXT NOT NULL,     -- username, LDAP DN, OAuth2 group name
    role TEXT NOT NULL DEFAULT 'viewer',  -- 'viewer' or 'editor'
    from_config INTEGER NOT NULL DEFAULT 0,
    UNIQUE(subject_type, subject_identifier)
);

-- Resolved per-user grants for private project access.
-- Created from global_access rules at login time or manually by admin.
CREATE TABLE IF NOT EXISTS global_access_grants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'viewer',
    source TEXT NOT NULL DEFAULT 'manual',  -- 'manual', 'ldap', 'oauth2'
    UNIQUE(user_id, source)
);

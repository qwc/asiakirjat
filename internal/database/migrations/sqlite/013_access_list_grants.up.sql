-- Resolved per-user grants for named access lists (issue #125).
--
-- A list member that names a user is matched by username when access is
-- checked, so it needs no grant. A member that names an LDAP or OAuth2 group
-- cannot be: group membership is only known while the user is signing in and
-- is not persisted, so the login sync writes the result here. Same split as
-- global_access / global_access_grants.
CREATE TABLE IF NOT EXISTS access_list_grants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    list_id INTEGER NOT NULL REFERENCES access_lists(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'viewer',  -- 'viewer' or 'editor'
    source TEXT NOT NULL DEFAULT 'ldap',  -- 'ldap' or 'oauth2'
    UNIQUE(list_id, user_id, source)
);

CREATE INDEX idx_access_list_grants_user ON access_list_grants(user_id);

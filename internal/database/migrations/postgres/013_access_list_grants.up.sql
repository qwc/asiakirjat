-- Resolved per-user grants for named access lists (issue #125). User members
-- are matched by username at check time; group members are resolved by the
-- LDAP/OAuth2 login sync and recorded here.
CREATE TABLE IF NOT EXISTS access_list_grants (
    id BIGSERIAL PRIMARY KEY,
    list_id BIGINT NOT NULL REFERENCES access_lists(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'viewer',
    source TEXT NOT NULL DEFAULT 'ldap',
    UNIQUE(list_id, user_id, source)
);

CREATE INDEX idx_access_list_grants_user ON access_list_grants(user_id);

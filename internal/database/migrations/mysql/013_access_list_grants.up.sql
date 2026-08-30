-- Resolved per-user grants for named access lists (issue #125). User members
-- are matched by username at check time; group members are resolved by the
-- LDAP/OAuth2 login sync and recorded here.
CREATE TABLE IF NOT EXISTS access_list_grants (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    list_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',
    source VARCHAR(50) NOT NULL DEFAULT 'ldap',
    UNIQUE KEY uq_list_user_source (list_id, user_id, source),
    KEY idx_access_list_grants_user (user_id),
    FOREIGN KEY (list_id) REFERENCES access_lists(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

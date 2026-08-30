-- Named access lists: a reusable set of subjects that a project's visibility
-- can point at (issue #125). Membership mirrors global_access rules, so a list
-- can be a single LDAP group, or an LDAP group plus named users.
CREATE TABLE IF NOT EXISTS access_lists (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uq_access_list_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS access_list_members (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    list_id BIGINT NOT NULL,
    subject_type VARCHAR(50) NOT NULL,
    subject_identifier VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',
    UNIQUE KEY uq_list_subject (list_id, subject_type, subject_identifier),
    FOREIGN KEY (list_id) REFERENCES access_lists(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE projects ADD COLUMN access_list_id BIGINT NULL,
    ADD CONSTRAINT fk_projects_access_list FOREIGN KEY (access_list_id)
        REFERENCES access_lists(id) ON DELETE RESTRICT;

CREATE INDEX idx_access_list_members_list ON access_list_members(list_id);

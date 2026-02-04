CREATE TABLE IF NOT EXISTS global_access (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    subject_type VARCHAR(50) NOT NULL,
    subject_identifier VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',
    from_config BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE KEY uq_subject (subject_type, subject_identifier)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS global_access_grants (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',
    source VARCHAR(50) NOT NULL DEFAULT 'manual',
    UNIQUE KEY uq_user_source (user_id, source),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS global_access (
    id BIGSERIAL PRIMARY KEY,
    subject_type TEXT NOT NULL,
    subject_identifier TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'viewer',
    from_config BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE(subject_type, subject_identifier)
);

CREATE TABLE IF NOT EXISTS global_access_grants (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'viewer',
    source TEXT NOT NULL DEFAULT 'manual',
    UNIQUE(user_id, source)
);

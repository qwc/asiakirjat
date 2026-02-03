ALTER TABLE api_tokens ADD COLUMN project_id BIGINT REFERENCES projects(id) ON DELETE CASCADE;
CREATE INDEX idx_api_tokens_project ON api_tokens(project_id);

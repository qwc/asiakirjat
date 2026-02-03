DROP INDEX idx_api_tokens_project ON api_tokens;
ALTER TABLE api_tokens DROP COLUMN project_id;

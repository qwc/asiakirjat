DROP INDEX IF EXISTS idx_api_tokens_project;
ALTER TABLE api_tokens DROP COLUMN project_id;

-- api_tokens.project_id has had no foreign key on MySQL since the column was
-- added (#155).
--
-- 003_token_project_scope.up.sql declares it as an inline column-level
-- REFERENCES clause. SQLite and PostgreSQL create a real constraint from that;
-- InnoDB parses it and throws it away. So deleting a project left its tokens
-- behind on MySQL, pointing at an id that no longer exists — and a token whose
-- scope names nothing is a token whose scope checks nothing the day that id is
-- reused by a restore.
--
-- Orphans have to go before the constraint can be added; they authenticate
-- nowhere as it is.

DELETE FROM api_tokens
    WHERE project_id IS NOT NULL
      AND project_id NOT IN (SELECT id FROM projects);

ALTER TABLE api_tokens
    ADD CONSTRAINT fk_api_tokens_project FOREIGN KEY (project_id)
        REFERENCES projects(id) ON DELETE CASCADE;

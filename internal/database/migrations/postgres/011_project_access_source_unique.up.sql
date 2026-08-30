-- Companion to the SQLite repair in this migration (issue #133). Postgres
-- already dropped the old per-user constraint in 002_auth_groups; this runs
-- idempotently so a database that somehow kept it is repaired too, and it
-- keeps the migration numbering aligned across dialects.
ALTER TABLE project_access DROP CONSTRAINT IF EXISTS project_access_project_id_user_id_key;

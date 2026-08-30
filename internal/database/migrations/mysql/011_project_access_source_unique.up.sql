-- Companion to the SQLite repair in this migration (issue #133). MySQL
-- dropped the old uq_project_user index correctly in 002_auth_groups, and
-- DROP INDEX has no IF EXISTS form to make a repair idempotent here, so this
-- migration only keeps the numbering aligned across dialects.
SELECT 1;

-- Make uploaded_by nullable and detach on user delete instead of blocking it.
-- Postgres auto-names FKs as <table>_<column>_fkey.

ALTER TABLE versions ALTER COLUMN uploaded_by DROP NOT NULL;
ALTER TABLE versions DROP CONSTRAINT versions_uploaded_by_fkey;
ALTER TABLE versions ADD CONSTRAINT versions_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE upload_logs ALTER COLUMN uploaded_by DROP NOT NULL;
ALTER TABLE upload_logs DROP CONSTRAINT upload_logs_uploaded_by_fkey;
ALTER TABLE upload_logs ADD CONSTRAINT upload_logs_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE SET NULL;

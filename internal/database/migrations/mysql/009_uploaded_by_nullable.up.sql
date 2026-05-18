-- Make uploaded_by nullable and detach on user delete instead of blocking it.
-- The FK for uploaded_by is the second FK declared on each table, so its
-- auto-generated name is `<table>_ibfk_2`.

ALTER TABLE versions MODIFY COLUMN uploaded_by BIGINT NULL;
ALTER TABLE versions DROP FOREIGN KEY versions_ibfk_2;
ALTER TABLE versions ADD CONSTRAINT versions_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE upload_logs MODIFY COLUMN uploaded_by INTEGER NULL;
ALTER TABLE upload_logs DROP FOREIGN KEY upload_logs_ibfk_2;
ALTER TABLE upload_logs ADD CONSTRAINT upload_logs_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE SET NULL;

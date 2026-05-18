-- Reverse 009. WARNING: MODIFY ... NOT NULL fails if any uploaded_by is NULL
-- (uploader deleted under the new schema). Run a manual cleanup first.

ALTER TABLE versions DROP FOREIGN KEY versions_uploaded_by_fkey;
ALTER TABLE versions MODIFY COLUMN uploaded_by BIGINT NOT NULL;
ALTER TABLE versions ADD CONSTRAINT versions_ibfk_2
    FOREIGN KEY (uploaded_by) REFERENCES users(id);

ALTER TABLE upload_logs DROP FOREIGN KEY upload_logs_uploaded_by_fkey;
ALTER TABLE upload_logs MODIFY COLUMN uploaded_by INTEGER NOT NULL;
ALTER TABLE upload_logs ADD CONSTRAINT upload_logs_ibfk_2
    FOREIGN KEY (uploaded_by) REFERENCES users(id);

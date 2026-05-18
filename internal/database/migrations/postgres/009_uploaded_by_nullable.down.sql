-- Reverse 009. WARNING: ALTER COLUMN SET NOT NULL fails if any uploaded_by
-- is NULL (uploader deleted under the new schema). Run a manual cleanup or
-- choose a sentinel user before reverting.

ALTER TABLE versions DROP CONSTRAINT versions_uploaded_by_fkey;
ALTER TABLE versions ADD CONSTRAINT versions_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES users(id);
ALTER TABLE versions ALTER COLUMN uploaded_by SET NOT NULL;

ALTER TABLE upload_logs DROP CONSTRAINT upload_logs_uploaded_by_fkey;
ALTER TABLE upload_logs ADD CONSTRAINT upload_logs_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES users(id);
ALTER TABLE upload_logs ALTER COLUMN uploaded_by SET NOT NULL;

-- Access rules confer viewer or editor, never admin: every path that writes
-- one has always coerced the value, and nothing reads 'admin' as anything
-- stronger than 'editor'. The stores now reject it outright (audit L-1), so
-- normalise any row that predates that or was written directly, otherwise
-- dropping the read-side 'admin' branches would revoke access instead of
-- being a no-op.
UPDATE project_access SET role = 'editor' WHERE role = 'admin';
UPDATE global_access SET role = 'editor' WHERE role = 'admin';
UPDATE global_access_grants SET role = 'editor' WHERE role = 'admin';
UPDATE auth_group_mappings SET role = 'editor' WHERE role = 'admin';

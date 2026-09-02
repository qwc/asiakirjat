-- Nothing to do: this dialect created a real foreign key on
-- api_tokens.project_id when the column was added, so a deleted project
-- already takes its tokens with it. The MySQL file at this number repairs the
-- constraint InnoDB silently discarded (#155). The numbering is kept in step
-- across dialects so a schema can be talked about by version.
SELECT 1;

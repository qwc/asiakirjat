-- Per-project regular expression naming the versions worth keeping (issue
-- #127). A version whose tag matches is kept indefinitely; anything else is
-- subject to the project's retention period.
--
-- NULL keeps the previous rule, where "worth keeping" meant "looks like a
-- semver tag", so existing projects are unaffected.
ALTER TABLE projects ADD COLUMN version_keep_pattern TEXT NULL;

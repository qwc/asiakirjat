# Version Retention

Uploading a build per branch or per CI run adds up. Retention deletes the versions a project has stopped caring about, while keeping the ones it hasn't.

Two settings on **Admin > Projects > Edit** decide this together:

| Field | Meaning |
|---|---|
| **Versions to Keep (regex)** | Versions whose tag matches are kept indefinitely |
| **Retention for Other Versions (days)** | Everything else is deleted once older than this |

## The Default

Leave the pattern empty and a project keeps anything that looks like a semver tag — `1.4.2`, `v2.0`, `v3.1.0-rc1` — and expires the rest. That is the historical behaviour, so projects that never touch these fields are unaffected.

Leave the retention days empty and the project follows the instance default from `retention.nonsemver_days` in `config.yaml`. Set it to `0` for unlimited: nothing is ever deleted automatically.

## Naming What to Keep

The pattern is a [RE2 regular expression](https://github.com/google/re2/wiki/Syntax) matched against the version tag. It matches anywhere in the tag unless you anchor it, so anchors are usually what you want:

| Pattern | Keeps |
|---|---|
| `^v\d+\.\d+\.\d+$` | Only full releases — `v1.2.3` stays, `v1.2.3-rc1` expires |
| `^v\d` | Anything starting with `v` and a digit, prereleases included |
| `^(v\d+\.\d+\.\d+\|stable\|main)$` | Releases plus two named branches |
| `^(?i)release-` | Tags starting with `release-`, any capitalisation |

A pattern that matches nothing means every version expires after the retention period. A pattern that matches everything means nothing is ever deleted, the same as a retention of `0`.

Invalid patterns are refused when you save. If one somehow reaches the database anyway, retention falls back to the semver rule rather than treating everything as expendable — the safe direction is always to keep more.

## When It Runs

Retention runs at startup, then hourly, and again right after an upload of a version the project does not keep. Deletion removes the version's files, its database record, and its search index entries. It cannot be undone, so it is worth setting the pattern before the retention days on a project with history you care about.

Pinning does **not** exempt a version from retention: the two features are
independent, so make sure your pattern also matches anything you have pinned.
See [Pin a Version as Latest](pin-versions.md).

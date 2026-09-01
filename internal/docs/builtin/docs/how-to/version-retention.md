# Version Retention

Uploading a build per branch or per CI run adds up. Retention deletes the versions a project has stopped caring about, while keeping the ones it hasn't.

Two settings on **Admin > Projects > Edit** decide this together:

| Field | Meaning |
|---|---|
| **Versions to Keep (regex)** | Versions whose tag matches are kept indefinitely |
| **Retention for Other Versions (days)** | Everything else is deleted once older than this |

## The Default

Leave the pattern empty and the project follows the instance-wide default from
`retention.keep_pattern` in `config.yaml`, which keeps **release numbers with an
optional `v` prefix**: `v1.2.3` and `2.0.0` stay indefinitely, while release
candidates (`v1.2.3-rc1`), dated builds (`2026-01-01`) and branch names (`main`)
expire once they pass the retention period.

If your projects tag releases as `v1.2` or `v2`, widen the instance default to
`^v?\d+(\.\d+)*$`, or give those projects their own pattern.

Leave the retention days empty and the project follows `retention.nonsemver_days`.
Set it to `0` — the shipped default — for unlimited: nothing is ever deleted
automatically, whatever the pattern says.

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

## Seeing What Will Go

While a project has a retention period, its detail page states the rule above
the version list and marks every version the rule does not keep with an
**Expires in N days** badge, counting from the version's upload date. A version
already past its window shows **Expires soon** — retention runs hourly, so it
goes on the next pass.

No badges and no notice means the project has no retention period: nothing is
deleted automatically, whatever the pattern says.

## When It Runs

Retention runs at startup, then hourly, and again right after an upload of a version the project does not keep. Deletion removes the version's files, its database record, and its search index entries. It cannot be undone, so it is worth setting the pattern before the retention days on a project with history you care about.

A **permanently pinned** version is never deleted by retention, whatever the
pattern says — a permanent pin is a statement that this is the version people
should land on. A **temporary** pin is not protected: it is cleared by the next
upload anyway, so it does not claim the version is worth keeping. See
[Pin a Version as Latest](pin-versions.md).

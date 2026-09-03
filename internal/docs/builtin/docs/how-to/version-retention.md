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
the version list and badges every version with what retention will do to it:

| Badge | Meaning |
|---|---|
| **Expires in N days** | The rule does not keep this version; the count runs from its upload date |
| **Expires today** | Already past its window — retention runs hourly, so it goes on the next pass |
| **No expiration** | Kept indefinitely, either by the pattern or by a pin |

No badges and no notice means the project has no retention period: nothing is
deleted automatically, whatever the pattern says, so there is nothing to mark.

## When It Runs

Retention runs at startup, then hourly, and again right after an upload of a version the project does not keep. Deletion removes the version's files, its database record, and its search index entries. It cannot be undone, so it is worth setting the pattern before the retention days on a project with history you care about.

A **pinned** version is never deleted by retention, whatever the pattern says —
a pin is a statement that this is the version people should land on, and
collecting it would leave readers on a different one.

This covers a **temporary** pin too, for as long as it is held. The pin is the
point: a temporary pin that gets deleted out from under its readers is a pin
that did nothing. The protection ends with the pin — the next upload clears a
temporary pin before retention runs, so a version already past its window goes
on that same pass. See [Pin a Version as Latest](pin-versions.md).

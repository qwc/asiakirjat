# Pin a Version as Latest

By default, the "latest" version of a project is determined by semantic version sorting (e.g., `v2.0.0` is newer than `v1.9.0`). You can override this by pinning any version as the latest.

## Prerequisites

- Editor or admin access to the project

## Pin Types

There are two ways to pin a version:

### Permanent Pin

A **permanent pin** persists across new uploads. Even when a newer version is uploaded, the pinned version remains the "latest". Use this when you want to keep a stable version visible while uploading pre-release or experimental versions.

### Temporary Pin

A **temporary pin** is automatically cleared when a new version is uploaded (re-uploads of the same version do not clear it). Use this when you want to temporarily highlight a specific version but return to normal semver sorting after the next upload.

## Pinning a Version

1. Navigate to the project page (`/project/{slug}`)
2. Find the version you want to pin in the version list
3. Click **Pin** for a permanent pin, or **Temp. pin** for a temporary pin
4. The pinned version will show a **Pinned** or **Temp. latest** badge

## Unpinning a Version

1. Navigate to the project page
2. Click the **Unpin** button next to the currently pinned version
3. The latest version will revert to semver sorting

## Effects of Pinning

When a version is pinned:

- The **frontpage** shows the pinned version as the latest for that project
- **Search** defaults to searching the pinned version (instead of the semver-sorted latest)
- The pinned version gets a badge in the version list

## Upload Log

Every upload (including re-uploads) is recorded in the project's upload log. Editors and admins can view the upload log on the project detail page by expanding the **Upload Log** section. The log shows:

- Upload date and time
- Version tag
- Content type (archive or PDF)
- Uploaded filename
- Username of the uploader
- Whether it was a new upload or a re-upload

# Uploading Documentation

This tutorial shows you how to upload documentation to Asiakirjat.

## Prerequisites

- A project created in Asiakirjat
- Editor or admin access to the project
- HTML documentation in a supported archive format

## Preparing Your Documentation

Asiakirjat serves static HTML documentation. Your archive should contain:

- An `index.html` file at the root (or in a single subdirectory)
- All HTML, CSS, JS, and image files your docs need

Supported archive formats:
- `.zip`
- `.tar.gz` / `.tgz`
- `.tar.bz2` / `.tbz2`
- `.tar.xz` / `.txz`
- `.7z`

## Uploading via Web Interface

1. Navigate to your project page (`/project/{slug}`)
2. Click **Upload New Version**
3. Fill in the form:
   - **Version Tag**: e.g., `v1.0.0`, `2.0.0`, `latest`
   - **Archive File**: Select your documentation archive
4. Click **Upload**

The archive is extracted and indexed for full-text search automatically.

## Uploading via API

For CI/CD integration, use the REST API:

```bash
curl -X POST \
  -H "Authorization: Bearer YOUR_API_TOKEN" \
  -F "archive=@docs.zip" \
  -F "version=v1.0.0" \
  https://your-server/api/project/my-docs/upload
```

See [API Tokens](../how-to/api-tokens.md) for token creation.

## Version Sorting

Versions are sorted using semantic versioning (semver) rules:

- `v2.0.0` appears before `v1.9.0`
- `latest` and `main` are sorted to the top
- Non-semver versions are sorted alphabetically

## Overwriting Versions

Uploading the same version tag again will:

1. Delete the existing version content
2. Remove it from the search index
3. Extract the new archive
4. Re-index for search

## What's Next?

- [CI/CD Integration](../how-to/ci-cd-integration.md) - Automate uploads
- [Archive Formats](../reference/archive-formats.md) - Detailed format info
- [Search](../explanation/search-indexing.md) - How search works

# Uploading Documentation

This tutorial shows you how to upload documentation to Asiakirjat.

## Prerequisites

- A project created in Asiakirjat
- Editor or admin access to the project
- An HTML documentation archive or a PDF file

## Preparing Your Documentation

Asiakirjat serves static HTML documentation or PDF documents. For HTML archives, your archive should contain:

- An `index.html` file at the root (or in a single subdirectory)
- All HTML, CSS, JS, and image files your docs need

Supported archive formats:
- `.zip`
- `.tar.gz` / `.tgz`
- `.tar.bz2` / `.tbz2`
- `.tar.xz` / `.txz`
- `.7z`
- `.pdf` (single PDF document)

The maximum upload size is **100 MB**.

## Uploading via Web Interface

1. Navigate to your project page (`/project/{slug}`)
2. Click **Upload New Version**
3. Fill in the form:
   - **Version Tag**: e.g., `v1.0.0`, `2.0.0`, `latest`
   - **Archive File**: Select your documentation archive or PDF file
4. Click **Upload**

The archive is extracted and indexed for full-text search automatically.

## PDF Upload

You can upload a single PDF file instead of an HTML archive. The PDF is:

- Stored as `document.pdf` in the version directory
- Displayed in the browser's built-in PDF viewer
- Text is extracted and indexed for full-text search

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

## Deleting Versions

To delete a version you no longer need:

1. Navigate to the project page (`/project/{slug}`)
2. Click the **Delete** button next to the version
3. Confirm the deletion

Deleting a version removes its files from storage and its entries from the search index. This action cannot be undone.

You must have editor or admin access to the project to delete versions.

## What's Next?

- [CI/CD Integration](../how-to/ci-cd-integration.md) - Automate uploads
- [Archive Formats](../reference/archive-formats.md) - Detailed format info
- [Search](../explanation/search-indexing.md) - How search works

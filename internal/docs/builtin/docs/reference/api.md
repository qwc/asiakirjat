# API Reference

REST API endpoints for programmatic access to Asiakirjat.

## Authentication

API requests require a Bearer token in the `Authorization` header:

```
Authorization: Bearer YOUR_API_TOKEN
```

See [API Tokens](../how-to/api-tokens.md) for token creation.

## Endpoints

### List Projects

List all projects accessible to the authenticated user.

```
GET /api/projects
```

**Response:**

```json
[
  {
    "id": 1,
    "slug": "my-project",
    "name": "My Project",
    "description": "Project description",
    "visibility": "custom",
    "created_at": "2024-01-15T10:30:00Z"
  }
]
```

The `visibility` field is one of: `public`, `private`, or `custom`.

**Status Codes:**
- `200 OK` - Success
- `401 Unauthorized` - Invalid or missing token

### List Versions

List all versions for a project.

```
GET /api/project/{slug}/versions
```

**Path Parameters:**
- `slug` - Project slug

**Response:**

```json
[
  {
    "id": 1,
    "tag": "v2.0.0",
    "created_at": "2024-01-20T14:00:00Z"
  },
  {
    "id": 2,
    "tag": "v1.0.0",
    "created_at": "2024-01-15T10:30:00Z"
  }
]
```

Versions are sorted by semantic version (newest first).

**Status Codes:**
- `200 OK` - Success
- `401 Unauthorized` - Invalid or missing token
- `403 Forbidden` - No access to project
- `404 Not Found` - Project not found

### Upload Documentation

Upload a documentation archive for a project version.

```
POST /api/project/{slug}/upload
```

**Path Parameters:**
- `slug` - Project slug

**Form Parameters:**
- `file` - Archive file (multipart/form-data)
- `tag` - Version tag (e.g., "v1.0.0", "latest")

**Example:**

```bash
curl -X POST \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "file=@docs.zip" \
  -F "tag=v1.0.0" \
  https://docs.example.com/api/project/my-project/upload
```

**Response:**

```json
{
  "message": "Documentation uploaded successfully",
  "project": "my-project",
  "version": "v1.0.0"
}
```

**Status Codes:**
- `200 OK` - Upload successful
- `400 Bad Request` - Invalid request (missing file, unsupported format)
- `401 Unauthorized` - Invalid or missing token
- `403 Forbidden` - No upload permission for project
- `404 Not Found` - Project not found

**Notes:**
- If the version already exists, it will be replaced
- Supported formats: .zip, .tar.gz, .tgz, .tar.bz2, .tbz2, .tar.xz, .txz, .7z
- The archive is extracted and indexed for search

### Search

Search documentation content.

```
GET /api/search?q={query}
```

**Query Parameters:**
- `q` - Search query (required)
- `project` - Filter by project slug (optional)
- `version` - Filter by version tag (optional)
- `all_versions` - Search all versions, not just latest (optional, default: false)
- `limit` - Results per page (optional, default: 20)
- `offset` - Pagination offset (optional, default: 0)

**Example:**

```bash
curl "https://docs.example.com/api/search?q=authentication&project=api-docs"
```

**Response:**

```json
{
  "results": [
    {
      "project_slug": "api-docs",
      "project_name": "API Documentation",
      "version_tag": "v2.0.0",
      "file_path": "auth/overview.html",
      "page_title": "Authentication Overview",
      "snippet": "...configure <mark>authentication</mark> for your API...",
      "url": "/project/api-docs/v2.0.0/auth/overview.html"
    }
  ],
  "total": 15
}
```

**Status Codes:**
- `200 OK` - Success
- `400 Bad Request` - Missing query parameter

## Error Responses

Errors return JSON with an error message:

```json
{
  "error": "Description of the error"
}
```

## Rate Limiting

The API does not currently implement rate limiting. Consider implementing rate limiting at the reverse proxy level for production deployments.

## Content Types

- Requests: `multipart/form-data` (for uploads) or URL parameters
- Responses: `application/json`

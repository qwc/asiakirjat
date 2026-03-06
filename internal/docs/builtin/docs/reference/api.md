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

### Create Project

Create a new project.

```
POST /api/projects
```

**Request Body (JSON):**
- `slug` (required) - URL-friendly identifier (lowercase alphanumeric with hyphens, 1-128 chars)
- `name` - Display name (defaults to slug)
- `description` - Project description
- `visibility` - One of `public`, `private`, `custom` (default: `private`)

**Example:**

```bash
curl -X POST \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"slug": "my-project", "name": "My Project"}' \
  https://docs.example.com/api/projects
```

**Response (201 Created):**

```json
{
  "slug": "my-project",
  "name": "My Project",
  "description": "",
  "visibility": "private"
}
```

**Status Codes:**
- `201 Created` - Project created
- `400 Bad Request` - Invalid slug or visibility
- `401 Unauthorized` - Invalid or missing token
- `403 Forbidden` - Requires admin or editor role
- `409 Conflict` - Project with this slug already exists

**Notes:**
- Requires a global (unscoped) API token — project-scoped tokens cannot create projects
- Non-admin creators are automatically granted editor access to the new project

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
    "tag": "v2.0.0",
    "content_type": "archive",
    "created_at": "2024-01-20T14:00:00Z"
  },
  {
    "tag": "v1.0.0",
    "content_type": "pdf",
    "created_at": "2024-01-15T10:30:00Z"
  }
]
```

The `content_type` field is either `"archive"` (HTML documentation) or `"pdf"` (single PDF document).

Versions are sorted by semantic version (newest first).

**Status Codes:**
- `200 OK` - Success
- `401 Unauthorized` - Invalid or missing token
- `403 Forbidden` - No access to project
- `404 Not Found` - Project not found

### Upload Documentation

Upload a documentation archive for a project version.

**Option 1: Project in URL path**

```
POST /api/project/{slug}/upload
```

**Path Parameters:**
- `slug` - Project slug

**Form Parameters:**
- `archive` - Archive file (multipart/form-data)
- `version` - Version tag (e.g., "v1.0.0", "latest")

**Example:**

```bash
curl -X POST \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "archive=@docs.zip" \
  -F "version=v1.0.0" \
  https://docs.example.com/api/project/my-project/upload
```

**Option 2: Project as form parameter**

```
POST /api/upload
```

**Form Parameters:**
- `project` - Project slug
- `archive` - Archive file (multipart/form-data)
- `version` - Version tag (e.g., "v1.0.0", "latest")

**Example:**

```bash
curl -X POST \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "project=my-project" \
  -F "archive=@docs.zip" \
  -F "version=v1.0.0" \
  https://docs.example.com/api/upload
```

**Response:**

```json
{
  "status": "ok",
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
- Both endpoints are functionally identical; choose based on your preference
- If the version already exists, it will be replaced
- Supported formats: .zip, .tar.gz, .tgz, .tar.bz2, .tbz2, .tar.xz, .txz, .7z, .pdf
- PDF files are stored directly; archives are extracted
- All uploads are indexed for full-text search
- Maximum upload size is 100 MB
- **Auto-create:** When `projects.auto_create` is enabled in config, uploading to a non-existent project slug will automatically create the project (requires admin or editor role and a global token). See [Configuration](configuration.md) for details.

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

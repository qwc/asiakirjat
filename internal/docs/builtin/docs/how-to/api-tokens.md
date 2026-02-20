# Using API Tokens

This guide explains how to create and use API tokens for programmatic access.

## Overview

API tokens allow automated systems (CI/CD pipelines, scripts) to upload documentation without interactive login. Tokens are associated with "robot" users.

## Token Types

### Robot User Tokens (Admin)

Created by admins for service accounts:

1. Go to **Admin > Robot Users**
2. Click **Create Robot User**
3. Enter a username (e.g., `ci-uploader`)
4. Click **Generate Token** on the robot user
5. Copy the token immediately (it's shown only once)

### Project-Scoped Tokens (Editor)

Editors and admins can create tokens scoped to specific projects:

1. Navigate to the project page (`/project/{slug}`)
2. Click **Manage Tokens** (or go to `/project/{slug}/tokens`)
3. Enter a token name and click **Create Token**
4. Copy the token immediately (it is shown only once)

Project-scoped tokens can **only** upload to that specific project. They cannot list other projects, upload to other projects, or perform any other actions. This makes them ideal for CI/CD pipelines where each project has its own deploy token.

## Using Tokens

Include the token in the `Authorization` header:

```bash
curl -X POST \
  -H "Authorization: Bearer YOUR_TOKEN_HERE" \
  -F "archive=@docs.zip" \
  -F "version=v1.0.0" \
  https://docs.example.com/api/project/my-project/upload
```

## API Endpoints

### Upload Documentation

```
POST /api/project/{slug}/upload
```

Parameters:
- `archive`: Archive file (multipart form)
- `version`: Version tag (e.g., "v1.0.0")

Response:
```json
{
  "message": "Documentation uploaded successfully",
  "project": "my-project",
  "version": "v1.0.0"
}
```

### List Projects

```
GET /api/projects
```

Returns projects accessible to the token's user.

### List Versions

```
GET /api/project/{slug}/versions
```

Returns versions for a specific project.

## Token Security

- Tokens are stored as SHA-256 hashes (the plain token is never stored)
- Tokens don't expire automatically
- Revoke tokens immediately if compromised
- Use project-scoped tokens when possible (principle of least privilege)

## Revoking Tokens

### Robot User Tokens

1. Go to **Admin > Robot Users**
2. Find the token in the robot's token list
3. Click **Revoke**

### Project-Scoped Tokens

1. Navigate to the project's token page
2. Find the token
3. Click **Revoke**

## CI/CD Examples

### GitHub Actions

```yaml
- name: Upload docs
  env:
    DOCS_TOKEN: ${{ secrets.ASIAKIRJAT_TOKEN }}
  run: |
    curl -X POST \
      -H "Authorization: Bearer $DOCS_TOKEN" \
      -F "archive=@dist/docs.zip" \
      -F "version=${{ github.ref_name }}" \
      https://docs.example.com/api/project/my-api/upload
```

### GitLab CI

```yaml
deploy_docs:
  script:
    - |
      curl -X POST \
        -H "Authorization: Bearer $DOCS_TOKEN" \
        -F "archive=@public.zip" \
        -F "version=$CI_COMMIT_TAG" \
        https://docs.example.com/api/project/my-api/upload
```

### Jenkins

```groovy
withCredentials([string(credentialsId: 'asiakirjat-token', variable: 'TOKEN')]) {
    sh '''
        curl -X POST \
            -H "Authorization: Bearer $TOKEN" \
            -F "archive=@docs.zip" \
            -F "version=${BUILD_TAG}" \
            https://docs.example.com/api/project/my-api/upload
    '''
}
```

## Troubleshooting

**401 Unauthorized**
- Check the token is correct
- Verify the token hasn't been revoked
- Ensure `Authorization: Bearer` prefix is present

**403 Forbidden**
- Robot user may not have access to the project
- Project-scoped token used for wrong project

**400 Bad Request**
- Check archive format is supported
- Verify `version` parameter is provided

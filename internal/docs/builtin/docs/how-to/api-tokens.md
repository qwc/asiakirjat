# Using API Tokens

This guide explains how to create and use API tokens for programmatic access.

## Overview

API tokens let automated systems — CI/CD pipelines, scripts — upload documentation without an interactive login.

A token is a **credential**, not an identity. Nothing but the token is sent: no username, no password. What the token does is name a **robot user**, and that robot is the identity everything else is decided from:

| | |
|---|---|
| **The token** | authenticates the request, and may be limited to one project |
| **The robot** | is what you grant access to, and what appears as the uploader of every version it pushes |

So the reach of a request is *what the robot has been granted*, narrowed by *what the token is limited to*. A token can never reach further than its robot; limiting it can only make it smaller.

A robot may hold several tokens — one per pipeline is the usual arrangement. They are independent credentials for one identity: revoke one and the others keep working, and the version history still names the robot.

## Token Types

### Robot User Tokens (Admin)

1. Go to **Admin > Robot Users**
2. Click **Create Robot User**
3. Enter a username (e.g. `ci-uploader`) and choose what it can reach — an organization or a project, and the role it holds there
4. Open the robot's card and click **Generate Token**
5. Copy the token immediately (it is shown only once)

A robot holds the **viewer** role and reaches what it has been granted, exactly like a person. A robot with no grants can authenticate and do nothing.

When generating a token you can:

- **Limit it to one project** — it then reaches only that project, whatever else the robot may be granted
- **Set an expiry** in days — leave it blank and the token never expires
- **Allow it to create projects** — needed only if `projects.auto_create` is on and this pipeline pushes to slugs that do not exist yet

### Project-Scoped Tokens (Editor)

Anyone who may upload to a project can issue a token for it:

1. Navigate to the project page (`/project/{slug}`)
2. Click **Manage Tokens** (or go to `/project/{slug}/tokens`)
3. Name the token, and name the robot it speaks for — the suggestion is `{slug}-bot`, and typing an existing robot's name gives that robot another credential
4. Copy the token immediately (it is shown only once)

The robot is created if it does not exist and granted **editor on that project alone**. The token is limited to the same project, so it can upload there and nowhere else, and it cannot create projects.

Tokens issued before this worked differently: they belonged to the person who pressed the button, carried that person's access, and would have died with their account. They still work and still show which account they speak for; reissue them against a robot when convenient.

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
- Tokens expire only if you give them an expiry; blank means never
- Revoke tokens immediately if compromised
- Grant the robot narrowly and limit the token to one project where you can — the two checks are independent, and both apply
- Creating projects is its own permission: a token has it only if it was issued with it

## Revoking Tokens

### Robot User Tokens

1. Go to **Admin > Robot Users**
2. Open the robot's card
3. Find the token and click **Revoke**

Revoking a token leaves the robot and its grants alone. To take away what the robot can reach, revoke the grant instead — the same card lists those.

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
- The robot has no grant on that project, or only a viewer one
- A project-scoped token was used for a different project
- The token was not issued with permission to create projects, and the slug does not exist yet

**409 Conflict on an upload to a new slug**
- The robot may create projects in more than one organization, so there is no single right place for it. Create the project explicitly with `POST /api/projects`.

**400 Bad Request**
- Check archive format is supported
- Verify `version` parameter is provided

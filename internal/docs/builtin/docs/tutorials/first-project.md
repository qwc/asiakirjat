# Create Your First Project

This tutorial shows you how to create a documentation project in Asiakirjat.

## Prerequisites

- Asiakirjat running and accessible
- Admin account credentials

## What is a Project?

A project in Asiakirjat represents a single documentation set. Each project can have multiple versions (e.g., v1.0, v2.0, latest) and can be public or private.

## Creating a Project

1. Log in as an admin user
2. Navigate to **Admin > Projects** (or go to `/admin/projects`)
3. Fill in the project form:
   - **Slug**: A URL-friendly identifier (e.g., `my-api-docs`)
   - **Name**: Human-readable name (e.g., "My API Documentation")
   - **Description**: Optional description
   - **Public**: Check if anyone can view without logging in
4. Click **Create**

## Project URL Structure

Once created, your project is accessible at:

```
/project/{slug}
```

For example: `/project/my-api-docs`

Documentation versions are served at:

```
/project/{slug}/{version}/{path}
```

For example: `/project/my-api-docs/v1.0/index.html`

## Assigning Access

For private projects, you need to grant access to users:

1. Go to **Admin > Projects**
2. Click **Edit** on your project
3. In the "Grant Access" section:
   - Select a user
   - Choose a role (viewer or editor)
   - Click **Grant**

### Roles

- **Viewer**: Can read documentation
- **Editor**: Can read and upload new versions

## What's Next?

- [Upload Documentation](uploading-docs.md) - Add content to your project
- [Roles and Permissions](../reference/roles-permissions.md) - Learn more about access control

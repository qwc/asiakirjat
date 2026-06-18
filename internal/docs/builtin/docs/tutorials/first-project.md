# Create Your First Project

This tutorial shows you how to create a documentation project in Asiakirjat.

## Prerequisites

- Asiakirjat running and accessible
- Admin or editor account credentials

## What is a Project?

A project in Asiakirjat represents a single documentation set. Each project can have multiple versions (e.g., v1.0, v2.0, latest) and has a visibility level: **public**, **private**, or **custom**.

## Creating a Project

1. Log in as an admin or editor user
2. Navigate to **Admin > Projects** (or go to `/admin/projects`)
3. Fill in the project form:
   - **Name**: Human-readable name (e.g., "My API Documentation")
   - **Slug**: A URL-friendly identifier (e.g., `my-api-docs`). With the **Auto slug** checkbox enabled (default), the slug is automatically derived from the name. Uncheck it to enter a custom slug.
   - **Description**: Optional Markdown description
   - **Visibility**: Choose access level:
     - **Public** — anyone can view without logging in (admin only)
     - **Private** — users in the global access list can view, plus anyone with an explicit per-project grant
     - **Custom** — only users with explicit per-project access can view (no org-wide path)
4. Click **Create**

Editors can create private and custom projects, but not public ones — public visibility bypasses all access checks, so reserving it for admins keeps editors from accidentally publishing internal docs to the open internet. When an editor creates a non-public project, they are automatically granted editor access to it, which lets them view *and* upload to their new project even without an org-wide global access grant.

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

For custom-visibility projects, you need to grant access to users individually:

1. Go to **Admin > Projects**
2. Click **Edit** on your project
3. In the "Grant Access" section:
   - Select a user
   - Choose a role (viewer or editor)
   - Click **Grant**

For private-visibility projects, access is normally controlled via the global access list (see [Manage Global Access](../how-to/manage-global-access.md)). You can additionally grant access to specific users from the project's edit page — useful for letting one external collaborator see a single private project without putting them on the org-wide list.

### Roles

- **Viewer**: Can read documentation
- **Editor**: Can read and upload new versions

> **Note for editors:** The admin project list is filtered to show only projects you have access to (admins see all projects). You can **Edit** and manage access for the projects you created — including the "Grant Access" steps above — without being an admin. You still can't make a project public; that's admin-only.

## What's Next?

- [Upload Documentation](uploading-docs.md) - Add content to your project
- [Roles and Permissions](../reference/roles-permissions.md) - Learn more about access control

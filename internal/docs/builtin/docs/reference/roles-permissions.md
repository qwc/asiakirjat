# Roles and Permissions

Understanding user roles and project access in Asiakirjat.

## User Roles

Each user has a global role that determines their base permissions.

### Admin

Full system access:
- Manage all projects (create, edit, delete)
- Manage all users (create, edit, delete)
- Manage robot users and API tokens
- Manage group mappings
- Upload to any project
- View all projects (public and private)
- Access admin panel
- Rebuild search index

### Editor

Limited management, broad access:
- Create projects (auto-granted editor access to non-public projects they create)
- **Edit and delete projects they created** (settings, slug, description, retention, visibility)
- **Manage per-project access** on projects they created (grant/revoke viewer/editor to other users)
- Access admin project list (filtered to only projects they have access to)
- View public projects
- View private projects they have access to
- Upload to projects where they have editor access
- Create project-scoped API tokens for their projects

Editors can only manage projects they **created** — not every project they can
view or upload to. They cannot make a project **public** (admin-only), and they
cannot manage users, robot users, group mappings, or global access.

### Viewer

Read-only access:
- View public projects
- View private projects they have access to

## Project Visibility

Projects have three visibility levels:

### Public

- Visible to everyone (including anonymous users)
- No login required to view

### Private

- Visible to authenticated users who appear in the global access list, **or** to any user with an explicit per-project access grant
- The global access list is configured in `config.yaml` under `access.private` or managed via the admin panel
- LDAP/OAuth2 group membership is resolved into access grants at login
- Per-project grants on a `private` project are additive — useful for letting a single outside collaborator into one project without adding them to the org-wide list

### Custom

- Visible only to users with explicit per-project access grants — no org-wide path
- Access is managed per-project in **Admin > Projects > Edit** (by an admin or by the editor who created the project)
- Strictly more restrictive than `private`: use this when even users on the global access list should be excluded by default

## Project Roles

When granting access to a custom-visibility project:

### Project Editor

- View the project documentation
- Upload new versions
- Delete versions
- Create project-scoped API tokens

### Project Viewer

- View the project documentation
- Cannot upload or modify

## Access Hierarchy

A user's effective access is determined by:

1. **Public visibility** — Anyone can view public projects
2. **Global admin role** — Full access to everything
3. **Private visibility + global access grant** — Access via global access list (config or LDAP/OAuth2 groups)
4. **Private visibility + per-project grant** — Access via an explicit per-project grant (manual, LDAP, or OAuth2 group mapping). This is also what lets a non-admin editor see a project they just created.
5. **Custom visibility + project grant** — Access via per-project grant (manual, LDAP, or OAuth2 group mapping)

## Global Access (Private Projects)

Global access controls who can view and upload to **private**-visibility projects. It can be configured two ways:

1. **Config file** — via the `access.private` section in `config.yaml` (see [Configuration Reference](configuration.md))
2. **Admin UI** — via **Admin > Global Access**, where admins can add rules for individual users, LDAP groups, or OAuth2 groups

Global access rules grant either viewer or editor access to **all** private projects. To *add* access to a specific private project for a user who isn't on the global access list, grant them per-project access from the project's edit page. To *restrict* a project so even users on the global access list need an explicit grant, switch its visibility to **custom**.

See [Manage Global Access](../how-to/manage-global-access.md) for a step-by-step guide.

## Group-Based Access

LDAP and OAuth2 authentication can map groups to project access:

```yaml
auth:
  ldap:
    project_groups:
      - group: "engineering"
        project: "api-docs"
        role: "editor"
      - group: "qa"
        project: "api-docs"
        role: "viewer"
```

Group mappings can also be managed in **Admin > Group Mappings**.

## Permission Matrix

| Action | Admin | Editor | Viewer |
|--------|-------|--------|--------|
| View public projects | Yes | Yes | Yes |
| View private projects (with global access) | Yes | Yes | Yes |
| View private projects (with per-project grant) | Yes | Yes | Yes |
| View custom projects (with project grant) | Yes | Yes | Yes |
| Upload to project (with grant) | Yes | Yes | No |
| Delete version (with grant) | Yes | Yes | No |
| Create project API tokens | Yes | Yes | No |
| Access admin panel (full) | Yes | No | No |
| Access admin project list (filtered) | Yes | Yes | No |
| Create projects | Yes | Yes | No |
| Edit/delete projects | All | Own only | No |
| Grant/revoke per-project access | All | Own only | No |
| Make a project public | Yes | No | No |
| Create/edit users | Yes | No | No |
| Manage robot users | Yes | No | No |
| Manage group mappings | Yes | No | No |
| Rebuild search index | Yes | No | No |

## Robot Users

Robot users are special accounts for API access:

- Cannot log in via web interface
- Can only authenticate via API token
- Created and managed by admins
- Typically given editor role

## Admin UI Features

The admin panel includes live filter inputs on the **Projects** and **Users** tables. Type to instantly filter rows by name, slug, visibility, username, email, role, or auth source. This is especially useful in larger environments with many entries.

The **Projects** table shows a **Created by** column identifying each project's
creator. Edit and Delete actions appear only on the rows you are allowed to
manage (admins see them on every project; editors see them on the projects they
created).

## Best Practices

1. **Principle of least privilege**: Grant minimum required access
2. **Use groups**: For organizations, use LDAP/OAuth2 groups over individual grants
3. **Project-scoped tokens**: Prefer project-scoped tokens over global robot tokens
4. **Regular audits**: Periodically review access grants and tokens
5. **Visibility choice**: Use `public` for open docs, `private` for organization-wide docs, `custom` for restricted docs

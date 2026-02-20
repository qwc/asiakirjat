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
- Access admin project list (filtered to only projects they have access to)
- View public projects
- View private projects they have access to
- Upload to projects where they have editor access
- Create project-scoped API tokens for their projects

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

- Visible to authenticated users who appear in the global access list
- The global access list is configured in `config.yaml` under `access.private` or managed via the admin panel
- LDAP/OAuth2 group membership is resolved into access grants at login

### Custom

- Visible only to users with explicit per-project access grants
- Access is managed per-project in **Admin > Projects > Edit**

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
4. **Custom visibility + project grant** — Access via per-project grant (manual, LDAP, or OAuth2 group mapping)

## Global Access (Private Projects)

Global access controls who can view and upload to **private**-visibility projects. It can be configured two ways:

1. **Config file** — via the `access.private` section in `config.yaml` (see [Configuration Reference](configuration.md))
2. **Admin UI** — via **Admin > Global Access**, where admins can add rules for individual users, LDAP groups, or OAuth2 groups

Global access rules grant either viewer or editor access to **all** private projects. For finer-grained control over individual projects, use **custom** visibility with per-project access grants instead.

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
| View custom projects (with project grant) | Yes | Yes | Yes |
| Upload to project (with grant) | Yes | Yes | No |
| Delete version (with grant) | Yes | Yes | No |
| Create project API tokens | Yes | Yes | No |
| Access admin panel (full) | Yes | No | No |
| Access admin project list (filtered) | Yes | Yes | No |
| Create projects | Yes | Yes | No |
| Edit/delete projects | Yes | No | No |
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

## Best Practices

1. **Principle of least privilege**: Grant minimum required access
2. **Use groups**: For organizations, use LDAP/OAuth2 groups over individual grants
3. **Project-scoped tokens**: Prefer project-scoped tokens over global robot tokens
4. **Regular audits**: Periodically review access grants and tokens
5. **Visibility choice**: Use `public` for open docs, `private` for organization-wide docs, `custom` for restricted docs

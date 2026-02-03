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
- View public projects
- View private projects they have access to
- Upload to projects where they have editor access
- Create project-scoped API tokens

### Viewer

Read-only access:
- View public projects
- View private projects they have access to

## Project Access

Projects can be public or private:

### Public Projects

- Visible to everyone (including anonymous users)
- No login required to view

### Private Projects

- Require explicit access grant
- Users must be granted viewer or editor role

## Project Roles

When granting access to a private project:

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

1. **Global admin role** - Full access to everything
2. **Project-specific grant** - Access to specific projects
3. **Group membership** - Access via LDAP/OAuth2 groups
4. **Public flag** - Anyone can view public projects

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
| View private projects (with grant) | Yes | Yes | Yes |
| Upload to project (with grant) | Yes | Yes | No |
| Delete version (with grant) | Yes | Yes | No |
| Create project API tokens | Yes | Yes | No |
| Access admin panel | Yes | No | No |
| Create/edit projects | Yes | No | No |
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

## Best Practices

1. **Principle of least privilege**: Grant minimum required access
2. **Use groups**: For organizations, use LDAP/OAuth2 groups over individual grants
3. **Project-scoped tokens**: Prefer project-scoped tokens over global robot tokens
4. **Regular audits**: Periodically review access grants and tokens
5. **Public vs private**: Make projects private unless external access is needed

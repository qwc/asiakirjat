# Manage Global Access

This guide explains how to manage the global access list, which controls who can view and upload to **private**-visibility projects.

## When to Use Global Access

Asiakirjat has three project visibility levels. Global access applies only to **private** visibility:

| Visibility | Access controlled by |
|---|---|
| **Public** | No restrictions — anyone can view |
| **Private** | Global access list (this guide) |
| **Custom** | Per-project access grants (see [Create Your First Project](../tutorials/first-project.md)) |

Use **private** visibility when you want all organization members (or a broad group) to see a project. Use **custom** visibility when you need fine-grained, per-project control.

## Managing Rules via Admin UI

1. Log in as an admin
2. Go to **Admin > Global Access** (or navigate to `/admin/global-access`)
3. To add a rule, fill in the form:
   - **Subject Type**: Choose `User`, `LDAP Group`, or `OAuth2 Group`
   - **Identifier**: The username, LDAP group DN, or OAuth2 group name
   - **Role**: `viewer` (read-only) or `editor` (read + upload)
4. Click **Create**

Rules take effect immediately. LDAP and OAuth2 group rules are resolved into per-user grants at login time.

To remove a rule, click **Delete** next to it. Config-sourced rules (from `config.yaml`) cannot be deleted through the UI — remove them from the config file instead.

## Managing Rules via Config File

You can also define global access rules in `config.yaml`:

```yaml
access:
  private:
    viewers:
      users: ["user1", "user2"]
      ldap_groups: ["cn=staff,ou=groups,dc=example,dc=com"]
      oauth2_groups: ["readers"]
    editors:
      users: ["lead-dev"]
      ldap_groups: ["cn=writers,ou=groups,dc=example,dc=com"]
      oauth2_groups: ["writers"]
```

Config-sourced rules appear in the admin UI marked as "from config" and cannot be deleted there.

## How It Works

When a user accesses a private project, Asiakirjat checks:

1. Is the user an admin? (admins always have full access)
2. Does the global access list include the user directly?
3. Does the global access list include an LDAP or OAuth2 group the user belongs to?

If any check succeeds, the user can view (viewer) or view and upload (editor) to **all** private projects.

## Global Access vs. Per-Project Access

| | Global access (private) | Per-project access (custom) |
|---|---|---|
| **Scope** | All private projects | One specific project |
| **Managed in** | Admin > Global Access or config | Admin > Projects > Edit |
| **Best for** | Organization-wide docs | Team-specific or restricted docs |
| **Group support** | LDAP + OAuth2 groups | LDAP + OAuth2 via group mappings |

## See Also

- [Roles and Permissions](../reference/roles-permissions.md) — Full permission matrix
- [Configuration Reference](../reference/configuration.md) — `access.private` config options
- [Configure LDAP](configure-ldap.md) — LDAP group integration

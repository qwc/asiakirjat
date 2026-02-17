# Configure LDAP Authentication

This guide shows you how to configure LDAP/Active Directory authentication.

## Overview

LDAP authentication allows users to log in with their corporate directory credentials. Asiakirjat supports:

- LDAP and LDAPS (TLS)
- Active Directory
- OpenLDAP
- Other LDAP-compatible directories

## Basic Configuration

Add the following to your `config.yaml`:

```yaml
auth:
  ldap:
    enabled: true
    url: "ldap://ldap.example.com:389"
    bind_dn: "cn=service,dc=example,dc=com"
    bind_password: "service-password"
    base_dn: "ou=users,dc=example,dc=com"
    user_filter: "(uid={{.Username}})"
```

## Configuration Options

| Option | Description |
|--------|-------------|
| `enabled` | Set to `true` to enable LDAP |
| `url` | LDAP server URL. Use `ldaps://` for TLS |
| `skip_verify` | Skip TLS certificate verification (for testing only) |
| `bind_dn` | Service account DN for searching users |
| `bind_password` | Service account password |
| `base_dn` | Base DN for user searches |
| `user_filter` | LDAP filter to find users. `{{.Username}}` is replaced with the login username |
| `admin_group` | LDAP group DN — members get admin role |
| `editor_group` | LDAP group DN — members get editor role |
| `viewer_group` | LDAP group DN — members get viewer role |

## Active Directory Example

```yaml
auth:
  ldap:
    enabled: true
    url: "ldaps://ad.corp.example.com:636"
    bind_dn: "CN=AsiakirjatSvc,OU=Service Accounts,DC=corp,DC=example,DC=com"
    bind_password: "secure-password"
    base_dn: "OU=Users,DC=corp,DC=example,DC=com"
    user_filter: "(sAMAccountName={{.Username}})"
```

## TLS Configuration

For secure connections, use `ldaps://` in the URL. To skip certificate verification (for testing only):

```yaml
auth:
  ldap:
    url: "ldaps://ldap.example.com:636"
    skip_verify: false        # Set true only for testing
```

## Role Assignment via Groups

Assign global roles based on LDAP group membership:

```yaml
auth:
  ldap:
    admin_group: "cn=admins,ou=groups,dc=example,dc=com"
    editor_group: "cn=editors,ou=groups,dc=example,dc=com"
    viewer_group: "cn=readers,ou=groups,dc=example,dc=com"
```

Members of `admin_group` are granted the admin role, `editor_group` the editor role, and `viewer_group` the viewer role.

## Project-Level Access via Groups

Grant project-specific access based on LDAP group membership:

```yaml
auth:
  ldap:
    project_groups:
      - group: "docs-team"
        project: "internal-docs"
        role: "editor"
      - group: "engineering"
        project: "api-docs"
        role: "viewer"
```

You can also manage group mappings in the Admin UI under **Group Mappings**.

## Testing

1. Restart Asiakirjat after config changes
2. Try logging in with an LDAP user
3. Check logs for authentication errors:

```bash
./asiakirjat -config config.yaml 2>&1 | grep -i ldap
```

## Troubleshooting

**"Invalid credentials"**
- Verify bind_dn and bind_password are correct
- Check that the user exists in user_base_dn

**"User not found"**
- Check user_filter syntax
- Verify user_base_dn is correct
- Test with `ldapsearch` command

**TLS errors**
- Ensure CA certificate is valid
- Try `skip_verify: true` temporarily to diagnose

## Environment Variables

All LDAP settings can be overridden with environment variables:

```bash
ASIAKIRJAT_LDAP_ENABLED=true
ASIAKIRJAT_LDAP_URL=ldaps://ldap.example.com:636
ASIAKIRJAT_LDAP_BIND_DN=cn=service,dc=example,dc=com
ASIAKIRJAT_LDAP_BIND_PASSWORD=secret
ASIAKIRJAT_LDAP_BASE_DN=ou=users,dc=example,dc=com
ASIAKIRJAT_LDAP_SKIP_VERIFY=false
```

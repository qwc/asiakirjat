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
    user_base_dn: "ou=users,dc=example,dc=com"
    user_filter: "(uid={{.Username}})"
    username_attr: "uid"
    email_attr: "mail"
    display_name_attr: "cn"
```

## Configuration Options

| Option | Description |
|--------|-------------|
| `enabled` | Set to `true` to enable LDAP |
| `url` | LDAP server URL. Use `ldaps://` for TLS |
| `bind_dn` | Service account DN for searching users |
| `bind_password` | Service account password |
| `user_base_dn` | Base DN for user searches |
| `user_filter` | LDAP filter to find users. `{{.Username}}` is replaced with the login username |
| `username_attr` | Attribute containing the username |
| `email_attr` | Attribute containing email address |
| `display_name_attr` | Attribute for display name |

## Active Directory Example

```yaml
auth:
  ldap:
    enabled: true
    url: "ldaps://ad.corp.example.com:636"
    bind_dn: "CN=AsiakirjatSvc,OU=Service Accounts,DC=corp,DC=example,DC=com"
    bind_password: "secure-password"
    user_base_dn: "OU=Users,DC=corp,DC=example,DC=com"
    user_filter: "(sAMAccountName={{.Username}})"
    username_attr: "sAMAccountName"
    email_attr: "mail"
    display_name_attr: "displayName"
```

## TLS Configuration

For secure connections:

```yaml
auth:
  ldap:
    url: "ldaps://ldap.example.com:636"
    tls:
      skip_verify: false        # Set true only for testing
      ca_cert: "/path/to/ca.pem"
```

## Group-Based Project Access

Automatically grant project access based on LDAP group membership:

```yaml
auth:
  ldap:
    group_base_dn: "ou=groups,dc=example,dc=com"
    group_filter: "(member={{.UserDN}})"
    group_attr: "cn"

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
ASIAKIRJAT_AUTH_LDAP_ENABLED=true
ASIAKIRJAT_AUTH_LDAP_URL=ldaps://ldap.example.com:636
ASIAKIRJAT_AUTH_LDAP_BIND_PASSWORD=secret
```

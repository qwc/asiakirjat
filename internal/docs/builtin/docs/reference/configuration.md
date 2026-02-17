# Configuration Reference

Complete reference for all Asiakirjat configuration options.

## Configuration File

Asiakirjat reads configuration from `config.yaml` (or path specified with `-config` flag).

## Environment Variables

All settings can be overridden with environment variables prefixed `ASIAKIRJAT_`. Use underscores for nested keys:

```bash
ASIAKIRJAT_SERVER_PORT=8080
ASIAKIRJAT_DATABASE_DRIVER=postgres
ASIAKIRJAT_AUTH_LDAP_ENABLED=true
```

## Server Settings

```yaml
server:
  address: "0.0.0.0"        # Listen address
  port: 8080                # Listen port
  base_path: ""             # URL prefix (e.g., "/docs")
  proxy_strip_path: false   # Set true if reverse proxy strips base_path
  log_level: "info"         # Logging level
```

| Option | Default | Description |
|--------|---------|-------------|
| `address` | `0.0.0.0` | IP address to bind to |
| `port` | `8080` | TCP port to listen on |
| `base_path` | `""` | URL prefix for all routes |
| `proxy_strip_path` | `false` | When true, routes are registered at root (for reverse proxies that strip the prefix) |
| `log_level` | `info` | Logging level: `debug`, `info`, `warn`, `error` |

## Database Settings

```yaml
database:
  driver: sqlite         # sqlite, postgres, or mysql
  dsn: "data/asiakirjat.db"
```

| Option | Default | Description |
|--------|---------|-------------|
| `driver` | `sqlite` | Database driver: `sqlite`, `postgres`, `mysql` |
| `dsn` | `data/asiakirjat.db` | Data source name / connection string |

### DSN Examples

**SQLite:**
```yaml
dsn: "data/asiakirjat.db"
```

**PostgreSQL:**
```yaml
dsn: "postgres://user:pass@localhost:5432/asiakirjat?sslmode=disable"
```

**MySQL:**
```yaml
dsn: "user:pass@tcp(localhost:3306)/asiakirjat?parseTime=true"
```

## Storage Settings

```yaml
storage:
  base_path: "data/projects"
```

| Option | Default | Description |
|--------|---------|-------------|
| `base_path` | `data/projects` | Directory for documentation files |

## Branding Settings

```yaml
branding:
  app_name: "Asiakirjat"          # Shown in header
  logo_url: ""                     # Logo image URL
  custom_css: ""                   # CSS filename in static/custom/
```

| Option | Default | Description |
|--------|---------|-------------|
| `app_name` | `Asiakirjat` | Application name in UI |
| `logo_url` | `""` | URL to logo image |
| `custom_css` | `""` | Filename of a custom CSS file placed in the `static/custom/` directory |

## Retention Settings

```yaml
retention:
  nonsemver_days: 0              # Days to keep non-semver versions (0 = unlimited)
```

| Option | Default | Description |
|--------|---------|-------------|
| `nonsemver_days` | `0` | Delete non-semver versions older than this many days. `0` means unlimited (no automatic deletion). |

Retention can also be configured per-project in the admin UI.

## Authentication Settings

### Session

```yaml
auth:
  session:
    cookie_name: "asiakirjat_session"
    max_age: 86400         # 24 hours in seconds
    secure: false          # Require HTTPS for cookies
```

### Initial Admin

```yaml
auth:
  initial_admin:
    username: "admin"
    password: "changeme"   # Change this!
```

Created on first startup if no users exist.

### Built-in Authentication

Built-in auth is always enabled. Users are stored in the database with bcrypt-hashed passwords.

### LDAP Authentication

```yaml
auth:
  ldap:
    enabled: false
    url: "ldap://ldap.example.com:389"
    skip_verify: false
    bind_dn: ""
    bind_password: ""
    base_dn: ""
    user_filter: "(uid={{.Username}})"
    admin_group: ""
    editor_group: ""
    viewer_group: ""
    recursive_groups: false
    group_prefix: ""          # CN prefix filter for recursion (empty = all)
    project_groups: []
```

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
| `recursive_groups` | Walk up each group's `memberOf` chain to resolve nested group memberships (default: `false`) |
| `group_prefix` | Only recurse into groups whose CN (common name) starts with this prefix (case-insensitive). For example, `"team-"` matches `cn=team-a,...` but not `cn=editors,...`. Groups outside the prefix still appear in the user's group list but are not expanded. Empty means all groups are followed. |
| `project_groups` | List of group-to-project access mappings |

See [Configure LDAP](../how-to/configure-ldap.md) for details.

### OAuth2 Authentication

```yaml
auth:
  oauth2:
    enabled: false
    client_id: ""
    client_secret: ""
    auth_url: ""
    token_url: ""
    userinfo_url: ""
    redirect_url: ""
    scopes: "openid profile email"
    groups_claim: "groups"
    admin_group: ""
    editor_group: ""
    viewer_group: ""
    project_groups: []
```

| Option | Description |
|--------|-------------|
| `enabled` | Set to `true` to enable OAuth2 |
| `client_id` | OAuth2 client ID |
| `client_secret` | OAuth2 client secret |
| `auth_url` | Authorization endpoint URL |
| `token_url` | Token endpoint URL |
| `userinfo_url` | UserInfo endpoint URL |
| `redirect_url` | Callback URL (must match provider config) |
| `scopes` | Space-separated list of OAuth2 scopes to request |
| `groups_claim` | Name of the claim containing group memberships (default: `"groups"`) |
| `admin_group` | OAuth2 group name — members get admin role |
| `editor_group` | OAuth2 group name — members get editor role |
| `viewer_group` | OAuth2 group name — members get viewer role |
| `project_groups` | List of group-to-project access mappings |

See [Configure OAuth2](../how-to/configure-oauth2.md) for details.

## Global Access Settings

The `access` section controls who can access projects with **private** visibility. Projects have three visibility levels:

| Visibility | Who can view | Governed by |
|---|---|---|
| `public` | Anyone, including anonymous users | — |
| `private` | Authenticated users in the global access list | `access.private` config + admin UI |
| `custom` | Only users with explicit per-project access | Per-project access grants |

```yaml
access:
  private:
    viewers:
      users: ["user1", "user2"]
      ldap_groups: ["cn=readers,ou=groups,dc=example,dc=com"]
      oauth2_groups: ["readers"]
    editors:
      users: ["editor1"]
      ldap_groups: ["cn=writers,ou=groups,dc=example,dc=com"]
      oauth2_groups: ["writers"]
```

| Option | Description |
|--------|-------------|
| `viewers.users` | Usernames granted viewer access to all private projects |
| `viewers.ldap_groups` | LDAP group DNs whose members get viewer access |
| `viewers.oauth2_groups` | OAuth2 group names whose members get viewer access |
| `editors.users` | Usernames granted editor access to all private projects |
| `editors.ldap_groups` | LDAP group DNs whose members get editor access |
| `editors.oauth2_groups` | OAuth2 group names whose members get editor access |

LDAP and OAuth2 group rules are resolved into per-user grants at login time.

## Complete Example

```yaml
server:
  address: "0.0.0.0"
  port: 8080
  base_path: ""
  log_level: "info"

database:
  driver: postgres
  dsn: "postgres://asiakirjat:secret@db:5432/asiakirjat?sslmode=disable"

storage:
  base_path: "/data/projects"

branding:
  app_name: "Company Docs"
  logo_url: "https://example.com/logo.png"

retention:
  nonsemver_days: 90

auth:
  session:
    cookie_name: "company_docs_session"
    max_age: 86400
    secure: true

  initial_admin:
    username: "admin"
    password: "initial-secure-password"

  ldap:
    enabled: true
    url: "ldaps://ldap.company.com:636"
    bind_dn: "cn=asiakirjat,ou=services,dc=company,dc=com"
    bind_password: "${LDAP_PASSWORD}"
    base_dn: "ou=users,dc=company,dc=com"
    user_filter: "(uid={{.Username}})"
    editor_group: "cn=writers,ou=groups,dc=company,dc=com"
    viewer_group: "cn=staff,ou=groups,dc=company,dc=com"

    project_groups:
      - group: "engineering"
        project: "api-docs"
        role: "viewer"

access:
  private:
    viewers:
      ldap_groups: ["cn=staff,ou=groups,dc=company,dc=com"]
    editors:
      ldap_groups: ["cn=writers,ou=groups,dc=company,dc=com"]
```

## Configuration Precedence

1. Environment variables (highest priority)
2. Config file
3. Default values (lowest priority)

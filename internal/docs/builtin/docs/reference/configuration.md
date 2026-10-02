# Configuration Reference

Complete reference for all Asiakirjat configuration options.

## Configuration File

Asiakirjat reads configuration from `config.yaml` (or path specified with `-config` flag).

## Environment Variables

Every scalar setting can be overridden with an `ASIAKIRJAT_`-prefixed
environment variable.

**The variable names are fixed, not derived from the YAML structure.** You
cannot build one by joining the nested YAML keys with underscores. An
unrecognized variable is ignored without a warning, so a wrong name looks
exactly like a setting that has no effect:

```bash
ASIAKIRJAT_DB_DRIVER=postgres        # correct
ASIAKIRJAT_DATABASE_DRIVER=postgres  # WRONG: ignored, no error

ASIAKIRJAT_LDAP_ENABLED=true         # correct
ASIAKIRJAT_AUTH_LDAP_ENABLED=true    # WRONG: ignored, no error

ASIAKIRJAT_STORAGE_PATH=/data        # correct
ASIAKIRJAT_STORAGE_BASE_PATH=/data   # WRONG: ignored, no error
```

Four names in particular do not follow their YAML path — see the table below:

| Config key | Variable |
|------------|----------|
| `database.driver` / `database.dsn` | `ASIAKIRJAT_DB_...`, not `..._DATABASE_...` |
| `server.log_level` | `ASIAKIRJAT_LOG_LEVEL`, with no `SERVER_` |
| `auth.initial_admin.*` | `ASIAKIRJAT_ADMIN_...`, with no `AUTH_INITIAL_` |
| `storage.base_path` | `ASIAKIRJAT_STORAGE_PATH`, not `..._BASE_PATH` |

### Value parsing

- **Booleans** are true for `true` (any capitalization) or `1`. Every other
  value, including `yes` and `on`, is false.
- **Integers** are parsed with base-10 digits only. A value that does not parse
  is ignored and the previous value is kept, without a warning.
- **Empty strings** are applied, not ignored: `ASIAKIRJAT_LDAP_BIND_DN=` sets
  the bind DN to empty rather than falling back to the config file.

### Complete list

| Config key | Environment variable |
|------------|----------------------|
| `server.address` | `ASIAKIRJAT_SERVER_ADDRESS` |
| `server.port` | `ASIAKIRJAT_SERVER_PORT` |
| `server.base_path` | `ASIAKIRJAT_SERVER_BASE_PATH` |
| `server.proxy_strip_path` | `ASIAKIRJAT_SERVER_PROXY_STRIP_PATH` |
| `server.log_level` | `ASIAKIRJAT_LOG_LEVEL` |
| `server.trusted_proxies` | `ASIAKIRJAT_SERVER_TRUSTED_PROXIES` |
| `database.driver` | `ASIAKIRJAT_DB_DRIVER` |
| `database.dsn` | `ASIAKIRJAT_DB_DSN` |
| `auth.initial_admin.username` | `ASIAKIRJAT_ADMIN_USERNAME` |
| `auth.initial_admin.password` | `ASIAKIRJAT_ADMIN_PASSWORD` |
| `auth.session.cookie_name` | `ASIAKIRJAT_SESSION_COOKIE_NAME` |
| `auth.session.max_age` | `ASIAKIRJAT_SESSION_MAX_AGE` |
| `auth.session.secure` | `ASIAKIRJAT_SESSION_SECURE` |
| `auth.ldap.enabled` | `ASIAKIRJAT_LDAP_ENABLED` |
| `auth.ldap.url` | `ASIAKIRJAT_LDAP_URL` |
| `auth.ldap.skip_verify` | `ASIAKIRJAT_LDAP_SKIP_VERIFY` |
| `auth.ldap.bind_dn` | `ASIAKIRJAT_LDAP_BIND_DN` |
| `auth.ldap.bind_password` | `ASIAKIRJAT_LDAP_BIND_PASSWORD` |
| `auth.ldap.base_dn` | `ASIAKIRJAT_LDAP_BASE_DN` |
| `auth.ldap.user_filter` | `ASIAKIRJAT_LDAP_USER_FILTER` |
| `auth.ldap.admin_group` | `ASIAKIRJAT_LDAP_ADMIN_GROUP` |
| `auth.ldap.editor_group` | `ASIAKIRJAT_LDAP_EDITOR_GROUP` |
| `auth.ldap.viewer_group` | `ASIAKIRJAT_LDAP_VIEWER_GROUP` |
| `auth.ldap.recursive_groups` | `ASIAKIRJAT_LDAP_RECURSIVE_GROUPS` |
| `auth.ldap.group_prefix` | `ASIAKIRJAT_LDAP_GROUP_PREFIX` |
| `auth.oauth2.enabled` | `ASIAKIRJAT_OAUTH2_ENABLED` |
| `auth.oauth2.client_id` | `ASIAKIRJAT_OAUTH2_CLIENT_ID` |
| `auth.oauth2.client_secret` | `ASIAKIRJAT_OAUTH2_CLIENT_SECRET` |
| `auth.oauth2.auth_url` | `ASIAKIRJAT_OAUTH2_AUTH_URL` |
| `auth.oauth2.token_url` | `ASIAKIRJAT_OAUTH2_TOKEN_URL` |
| `auth.oauth2.userinfo_url` | `ASIAKIRJAT_OAUTH2_USERINFO_URL` |
| `auth.oauth2.redirect_url` | `ASIAKIRJAT_OAUTH2_REDIRECT_URL` |
| `auth.oauth2.scopes` | `ASIAKIRJAT_OAUTH2_SCOPES` |
| `auth.oauth2.groups_claim` | `ASIAKIRJAT_OAUTH2_GROUPS_CLAIM` |
| `auth.oauth2.admin_group` | `ASIAKIRJAT_OAUTH2_ADMIN_GROUP` |
| `auth.oauth2.editor_group` | `ASIAKIRJAT_OAUTH2_EDITOR_GROUP` |
| `auth.oauth2.viewer_group` | `ASIAKIRJAT_OAUTH2_VIEWER_GROUP` |
| `storage.base_path` | `ASIAKIRJAT_STORAGE_PATH` |
| `retention.nonsemver_days` | `ASIAKIRJAT_RETENTION_NONSEMVER_DAYS` |
| `retention.keep_pattern` | `ASIAKIRJAT_RETENTION_KEEP_PATTERN` |
| `branding.app_name` | `ASIAKIRJAT_BRANDING_APP_NAME` |
| `branding.logo_url` | `ASIAKIRJAT_BRANDING_LOGO_URL` |
| `branding.custom_css` | `ASIAKIRJAT_BRANDING_CUSTOM_CSS` |
| `projects.auto_create` | `ASIAKIRJAT_PROJECTS_AUTO_CREATE` |

### Settings with no environment override

Settings that hold a list have no environment variable and can only be set in
`config.yaml`:

- the whole `access` section (`groups`, `grants`, `private`)
- `auth.ldap.project_groups` and `auth.oauth2.project_groups`

### Keeping secrets out of the config file

`config.yaml` has **no variable interpolation**. Writing

```yaml
bind_password: "${LDAP_PASSWORD}"   # does NOT work
```

stores the literal string `${LDAP_PASSWORD}` as the password, and the LDAP bind
then fails with an authentication error that gives no hint as to why.

To keep a secret out of the file, leave the key out of the YAML entirely and
set its environment variable instead. Environment variables are applied after
the file is read, so an absent key is simply filled in. This works for
`ASIAKIRJAT_LDAP_BIND_PASSWORD`, `ASIAKIRJAT_OAUTH2_CLIENT_SECRET`,
`ASIAKIRJAT_ADMIN_PASSWORD` and `ASIAKIRJAT_DB_DSN`.

In Kubernetes, put the config file in a ConfigMap and the secrets in a Secret:

```yaml
spec:
  template:
    spec:
      containers:
        - name: asiakirjat
          env:
            - name: ASIAKIRJAT_LDAP_BIND_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: asiakirjat-secrets
                  key: ldap-bind-password
```

With Docker Compose, use `env_file` or Compose secrets rather than putting the
value in `config.yaml`.

## Server Settings

```yaml
server:
  address: "0.0.0.0"        # Listen address
  port: 8080                # Listen port
  base_path: ""             # URL prefix (e.g., "/docs")
  proxy_strip_path: false   # Set true if reverse proxy strips base_path
  log_level: "info"         # Logging level
  trusted_proxies: ""       # Trusted proxy IPs/CIDRs for X-Forwarded-For
```

| Option | Default | Description |
|--------|---------|-------------|
| `address` | `0.0.0.0` | IP address to bind to |
| `port` | `8080` | TCP port to listen on |
| `base_path` | `""` | URL prefix for all routes |
| `proxy_strip_path` | `false` | When true, routes are registered at root (for reverse proxies that strip the prefix) |
| `log_level` | `info` | Logging level: `debug`, `info`, `warn`, `error` |
| `trusted_proxies` | `""` | Comma-separated CIDR ranges or IPs whose `X-Forwarded-For` headers are honored when deriving the client IP for rate limiting. Empty (default) means the header is ignored and the connecting peer is always used. Set this when behind a reverse proxy so the login rate limiter protects against the real client, not the proxy. |

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

Asiakirjat appends `_pragma=foreign_keys(1)` and `_pragma=journal_mode(WAL)` to
a SQLite DSN so both apply to every pooled connection. Foreign keys must stay
enabled: without them a deleted project leaves its versions, upload logs,
access rows and API tokens behind, and the orphans break later schema
migrations. If you set either pragma yourself your value is kept, and startup
fails with a clear error if foreign keys end up disabled.

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
  nonsemver_days: 0                  # Days to keep non-matching versions (0 = unlimited)
  keep_pattern: "^v?\d+\.\d+\.\d+$"  # Versions kept indefinitely
```

| Option | Default | Description |
|--------|---------|-------------|
| `nonsemver_days` | `0` | Delete versions that do not match `keep_pattern`, once older than this many days. `0` means unlimited (no automatic deletion). |
| `keep_pattern` | `^v?\d+\.\d+\.\d+$` | Regular expression naming the versions kept indefinitely. The default keeps release numbers — `v1.2.3`, `2.0.0` — and lets release candidates, dated builds and branch names expire. Widen it to `^v?\d+(\.\d+)*$` if you also tag `v1.2` or `v2`. |

Retention can also be configured per-project in the admin UI, where a project can
additionally name the versions it wants to keep with a regular expression — see
[Version Retention](../how-to/version-retention.md).

## Project Settings

```yaml
projects:
  auto_create: false             # Auto-create projects on first upload
```

| Option | Default | Description |
|--------|---------|-------------|
| `auto_create` | `false` | When enabled, uploading to a non-existent project slug automatically creates the project with private visibility. Only admins and editors can trigger auto-creation. |

When auto-create is enabled:
- The project is created with the slug as its name and `private` visibility
- Non-admin creators are automatically granted editor access
- API uploads require a global (unscoped) token since no project exists yet for scope validation
- The web UI upload form also supports auto-creation for logged-in editors/admins

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

projects:
  auto_create: true

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
    # Set via ASIAKIRJAT_LDAP_BIND_PASSWORD instead of writing it here.
    # This file has no variable interpolation: "${LDAP_PASSWORD}" would
    # be stored verbatim as the password.
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

Each setting is resolved independently, so a config file may set some values
while the environment supplies others. A key that is absent from the config
file is not an error; it keeps its default until an environment variable or the
admin UI changes it.

A missing config file is also not an error: Asiakirjat starts on defaults plus
whatever the environment provides. If you pass `-config` a path that does not
exist, it is skipped silently, so check the path when settings seem to be
ignored wholesale.

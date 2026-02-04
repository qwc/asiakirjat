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
  host: "0.0.0.0"        # Listen address
  port: 8080             # Listen port
  base_path: ""          # URL prefix (e.g., "/docs")
```

| Option | Default | Description |
|--------|---------|-------------|
| `host` | `0.0.0.0` | IP address to bind to |
| `port` | `8080` | TCP port to listen on |
| `base_path` | `""` | URL prefix for all routes |

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
  base_path: "data/docs"
```

| Option | Default | Description |
|--------|---------|-------------|
| `base_path` | `data/docs` | Directory for documentation files |

## Branding Settings

```yaml
branding:
  app_name: "Asiakirjat"          # Shown in header
  logo_url: ""                     # Logo image URL
  custom_css: ""                   # Additional CSS
```

| Option | Default | Description |
|--------|---------|-------------|
| `app_name` | `Asiakirjat` | Application name in UI |
| `logo_url` | `""` | URL to logo image |
| `custom_css` | `""` | Custom CSS rules |

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
    bind_dn: ""
    bind_password: ""
    user_base_dn: ""
    user_filter: "(uid={{.Username}})"
    username_attr: "uid"
    email_attr: "mail"
    display_name_attr: "cn"

    # TLS settings (for ldaps://)
    tls:
      skip_verify: false
      ca_cert: ""

    # Group-based access
    group_base_dn: ""
    group_filter: "(member={{.UserDN}})"
    group_attr: "cn"

    project_groups: []
```

See [Configure LDAP](../how-to/configure-ldap.md) for details.

### OAuth2 Authentication

```yaml
auth:
  oauth2:
    enabled: false
    provider: "generic"
    client_id: ""
    client_secret: ""
    auth_url: ""
    token_url: ""
    userinfo_url: ""
    redirect_url: ""
    scopes:
      - openid
      - profile
      - email

    claims:
      username: "preferred_username"
      email: "email"
      display_name: "name"
      groups: "groups"

    project_groups: []
```

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
  host: "0.0.0.0"
  port: 8080
  base_path: ""

database:
  driver: postgres
  dsn: "postgres://asiakirjat:secret@db:5432/asiakirjat?sslmode=disable"

storage:
  base_path: "/data/docs"

branding:
  app_name: "Company Docs"
  logo_url: "https://example.com/logo.png"

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
    user_base_dn: "ou=users,dc=company,dc=com"
    user_filter: "(uid={{.Username}})"

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

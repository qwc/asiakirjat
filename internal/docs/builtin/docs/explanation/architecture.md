# Architecture Overview

Understanding how Asiakirjat is structured and how components interact.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    HTTP Server                          │
│   ┌─────────────┬─────────────┬─────────────────────┐  │
│   │   Static    │   Handler   │    Doc Server       │  │
│   │   Files     │  (Routes)   │  (File Serving)     │  │
│   └─────────────┴──────┬──────┴─────────────────────┘  │
│                        │                                │
│   ┌────────────────────┼────────────────────────────┐  │
│   │                    │                            │  │
│   │  ┌─────────┐  ┌────┴────┐  ┌─────────────────┐ │  │
│   │  │ Session │  │  Auth   │  │ Search Index    │ │  │
│   │  │ Manager │  │ Chain   │  │ (Bleve)         │ │  │
│   │  └─────────┘  └────┬────┘  └─────────────────┘ │  │
│   │                    │                            │  │
│   │       ┌────────────┼────────────┐              │  │
│   │       │            │            │              │  │
│   │  ┌────┴────┐  ┌────┴────┐  ┌───┴────┐        │  │
│   │  │ Builtin │  │  LDAP   │  │ OAuth2 │        │  │
│   │  │  Auth   │  │  Auth   │  │  Auth  │        │  │
│   │  └─────────┘  └─────────┘  └────────┘        │  │
│   │                                               │  │
│   └───────────────────┬──────────────────────────┘  │
│                       │                              │
│   ┌───────────────────┴──────────────────────────┐  │
│   │              Store Layer                      │  │
│   │  ┌─────────┬─────────┬─────────┬──────────┐ │  │
│   │  │ Project │  User   │ Version │  Token   │ │  │
│   │  │  Store  │  Store  │  Store  │  Store   │ │  │
│   │  └────┬────┴────┬────┴────┬────┴────┬─────┘ │  │
│   │       └─────────┴─────────┴─────────┘        │  │
│   │                      │                        │  │
│   │              ┌───────┴───────┐               │  │
│   │              │   Database    │               │  │
│   │              │ SQLite/PG/MY  │               │  │
│   │              └───────────────┘               │  │
│   └──────────────────────────────────────────────┘  │
│                                                      │
│   ┌──────────────────────────────────────────────┐  │
│   │            File Storage                       │  │
│   │  ┌─────────────────────────────────────────┐ │  │
│   │  │ /data/docs/{project}/{version}/...      │ │  │
│   │  └─────────────────────────────────────────┘ │  │
│   └──────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

## Package Structure

```
asiakirjat/
├── main.go                 # Entry point, wiring
├── static/                 # CSS, JS, images
├── internal/
│   ├── config/            # YAML config, env vars
│   ├── database/          # Models, migrations
│   ├── store/             # Repository interfaces
│   │   └── sql/          # SQL implementations
│   ├── auth/              # Authentication
│   ├── docs/              # Archive, storage, search
│   ├── handler/           # HTTP handlers
│   └── templates/         # HTML templates
└── vendor/                # Dependencies
```

## Key Components

### Handler Layer

The `handler` package contains all HTTP handlers and middleware:

- **Middleware**: Session loading, authentication, logging, recovery
- **Public routes**: Login, frontpage, project viewing
- **Admin routes**: User/project management
- **API routes**: REST endpoints for programmatic access

Handlers receive a `Deps` struct with all dependencies (stores, auth, templates).

### Store Layer

Repository pattern with interfaces in `store/` and SQL implementations in `store/sql/`:

- `ProjectStore`: CRUD for projects
- `VersionStore`: CRUD for versions
- `UserStore`: User management
- `SessionStore`: Session persistence
- `ProjectAccessStore`: Access grants
- `TokenStore`: API tokens
- `AuthGroupMappingStore`: LDAP/OAuth2 group mappings

### Authentication

Auth is handled by a chain of authenticators in order:

1. **Builtin**: Password hash in database
2. **LDAP**: Directory server authentication
3. **OAuth2**: OIDC provider authentication

The first authenticator to return success wins. Session management is separate from authentication.

### Storage

Documentation files are stored on the filesystem:

```
{base_path}/
├── project-a/
│   ├── v1.0.0/
│   │   ├── index.html
│   │   └── ...
│   └── v2.0.0/
│       └── ...
└── project-b/
    └── latest/
        └── ...
```

### Search Index

Full-text search uses Bleve, stored at `{base_path}/.search-index/`:

- Indexes HTML content (excluding scripts, styles, nav)
- Stores project/version metadata
- Supports phrase, fuzzy, and filtered queries

## Request Flow

### Viewing Documentation

```
1. Request: GET /project/api-docs/v1.0.0/auth.html
2. Session middleware loads user (if logged in)
3. Handler checks project access
4. File served from storage path
```

### Uploading Documentation

```
1. Request: POST /api/project/api-docs/upload
2. Token authentication middleware
3. Check user has editor access
4. Create/update version record
5. Extract archive to storage
6. Index HTML files for search
7. Return success response
```

### Authentication Flow

```
1. Request: POST /login (username, password)
2. Try each authenticator in order
3. First success creates session
4. Session stored in database
5. Cookie set with session ID
6. Redirect to frontpage
```

## Database Schema

Core tables:
- `projects`: Project metadata
- `versions`: Version records linked to projects
- `users`: User accounts
- `sessions`: Active sessions
- `project_access`: User-project grants
- `api_tokens`: API authentication tokens
- `auth_group_mappings`: External group to project mappings

## Configuration Flow

```
1. Load config.yaml
2. Apply environment variable overrides
3. Validate required settings
4. Initialize components with config
```

## Design Principles

1. **Dependency injection**: All dependencies passed via structs
2. **Interface-based stores**: Easy testing and DB flexibility
3. **Auth chain**: Extensible authentication
4. **Static file serving**: Efficient for documentation
5. **Embedded assets**: Single binary deployment

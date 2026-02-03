# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Asiakirjat (Finnish for "documents") is a self-hosted HTML documentation server with user management, authentication (built-in, LDAP, OAuth2), version management, and full-text search using Bleve.

## Build and Test Commands

```bash
# Build
CGO_ENABLED=0 go build -mod=vendor -ldflags="-s -w" -o asiakirjat .

# Run all tests
go test -mod=vendor -count=1 ./...

# Run tests for a specific package
go test -mod=vendor -count=1 ./internal/handler/...

# Run a single test
go test -mod=vendor -count=1 -run TestHandleLogin ./internal/handler/...

# Run with config
./asiakirjat -config config.yaml
```

## Architecture

### Package Structure

- **main.go**: Entry point - wires dependencies, runs migrations, starts server
- **internal/config**: YAML config with environment variable overrides (ASIAKIRJAT_*)
- **internal/database**: Models, migrations (sqlite/postgres/mysql), dialect detection
- **internal/store**: Repository interfaces; **internal/store/sql**: SQL implementations
- **internal/auth**: Authenticators (builtin, LDAP, OAuth2) and session management
- **internal/docs**: Archive extraction, document serving, Bleve search indexing
- **internal/handler**: HTTP handlers and middleware (74 routes)
- **internal/templates**: HTML templates with Goldmark markdown rendering

### Key Patterns

**Dependency Injection**: Handler receives all dependencies via `Deps` struct.

**Repository Pattern**: Store interfaces (ProjectStore, UserStore, etc.) with SQL implementations.

**Authentication Chain**: Multiple authenticators tried in order; first success wins.

**Context-Based User**: `auth.ContextWithUser(ctx, user)` / `auth.UserFromContext(ctx)`

### Database

Supports SQLite (default), PostgreSQL, and MySQL. Migrations in `internal/database/migrations/{dialect}/` run automatically on startup.

### Roles

- **admin**: Full access to admin panel, all projects
- **editor**: Can upload documentation to assigned projects
- **viewer**: Read-only access to assigned projects

### Archive Formats

Supports: .zip, .tar.gz, .tgz, .tar.bz2, .tbz2, .tar.xz, .txz, .7z

## Configuration

Copy `config.yaml.example` to `config.yaml`. All settings can be overridden with environment variables prefixed `ASIAKIRJAT_` (e.g., `ASIAKIRJAT_DB_DRIVER`).

## AI Contribution Policy

Mark commits/PRs created with AI assistance. Keep commits under ~250 changed lines for human reviewability.

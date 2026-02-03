# Asiakirjat Documentation

Welcome to Asiakirjat (Finnish for "documents"), a self-hosted HTML documentation server with user management, authentication, version control, and full-text search.

## Quick Start

New to Asiakirjat? Start with these tutorials:

- [Getting Started](tutorials/getting-started.md) - Set up your first Asiakirjat instance
- [Create Your First Project](tutorials/first-project.md) - Create a documentation project
- [Upload Documentation](tutorials/uploading-docs.md) - Upload your first documentation archive

## How-To Guides

Step-by-step guides for common tasks:

- [Configure LDAP Authentication](how-to/configure-ldap.md)
- [Configure OAuth2 Authentication](how-to/configure-oauth2.md)
- [Use API Tokens](how-to/api-tokens.md)
- [CI/CD Integration](how-to/ci-cd-integration.md)

## Reference

Technical reference documentation:

- [Configuration Reference](reference/configuration.md) - All configuration options
- [API Reference](reference/api.md) - REST API endpoints
- [Roles and Permissions](reference/roles-permissions.md) - User roles explained
- [Archive Formats](reference/archive-formats.md) - Supported archive types

## Explanation

Background information and concepts:

- [Architecture Overview](explanation/architecture.md) - How Asiakirjat works
- [Authentication System](explanation/authentication.md) - Auth providers and flow
- [Search Indexing](explanation/search-indexing.md) - Full-text search internals

## Key Features

- **Version Management** - Upload multiple versions of documentation, semver-sorted
- **Full-Text Search** - Search across all projects and versions using Bleve
- **Multiple Auth Methods** - Built-in users, LDAP, OAuth2/OIDC
- **Role-Based Access** - Admin, editor, and viewer roles with project-level permissions
- **Archive Support** - Upload .zip, .tar.gz, .tgz, .tar.bz2, .tbz2, .tar.xz, .txz, .7z
- **API Access** - REST API with token authentication for CI/CD integration

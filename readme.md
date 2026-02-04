# asiakirjat

## What does the word mean?

It is finnish (Suomi) and means "documents" or "documentation".

## What is the purpose of this application?

Serve HTML documentation with user management, authorization and authentication.

Backends for user management shall be:

- built-in
- LDAP
- oauth2

Documentation blobs can be uploaded as archives (whatever format), versioned with version tags for multiple projects.

A stretch-goal is that you can search through all documentations stored, not only by title, but in the content.

## Features

- **Multi-project hosting** with slug-based URLs and per-project versioning
- **Three-tier visibility**: public (anonymous), private (global access list), custom (per-project grants)
- **Authentication**: built-in (bcrypt), LDAP, OAuth2/OIDC — used simultaneously
- **Role-based access**: admin, editor, viewer at global and per-project level
- **Group mapping**: LDAP/OAuth2 groups to project access and global access roles
- **Full-text search** (Bleve) across all documentation with project/version filtering
- **Archive upload**: .zip, .tar.gz, .tgz, .tar.bz2, .tbz2, .tar.xz, .txz, .7z
- **REST API** with Bearer token auth: project listing, version listing, upload, search
- **Robot users**: API-only accounts with project-scoped tokens for CI/CD
- **Multi-database**: SQLite (default), PostgreSQL, MySQL with auto-migrations
- **Admin panel**: manage projects, users, robots, group mappings, search reindex
- **Branding**: custom app name, logo, CSS
- **Self-documenting**: deployable built-in documentation
- **Single binary**, Docker-ready

# AI Policy

## Author Statement

Yes. I used AI to almost vibe-code this application.

Why? I am a single person with limited time - family, other hobbies, ...
I do not have the time to write all by my own hands, despite the fact that I would love to.

I use AI responsibly. I do understand that sentence like, that I use AI almost only in my own field of expertise, so that I can actually continue also without AI, if the need arises, the AI systems fail or vanish or the world goes down the drain.

AI coding buys me time and gets my software ideas faster into reality, I see it as benefit as a tool for programming creativity.

## AI Policy for contributions

Yes you may use AI to contribute.

If you follow these two rules:

- Always mark your commits + PRs that you created the code with the help of AI as your wingman
- Make commits in a size a human can still review within minutes.
    - Max ~250 changed lines per commit
    - If your contribution is larger, instruct your AI to commit the work in as much bite-sized commits as necessary, so that you, yourself, can still follow, what is changed.

Because the reviewer of the PRs will still be a human, who wants to understand what's going on.

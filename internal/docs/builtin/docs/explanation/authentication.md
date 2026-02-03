# Authentication System

How authentication works in Asiakirjat.

## Overview

Asiakirjat supports multiple authentication methods that can be used simultaneously. Users can authenticate via any enabled method, and the system tries each in order until one succeeds.

## Authentication Flow

```
User submits credentials
        │
        ▼
┌───────────────────┐
│ Builtin Auth      │──► Success ──► Create Session
│ (check database)  │
└────────┬──────────┘
         │ Fail
         ▼
┌───────────────────┐
│ LDAP Auth         │──► Success ──► Create/Update User ──► Create Session
│ (if enabled)      │
└────────┬──────────┘
         │ Fail
         ▼
┌───────────────────┐
│ OAuth2 Auth       │──► Success ──► Create/Update User ──► Create Session
│ (if enabled)      │
└────────┬──────────┘
         │ Fail
         ▼
    Return Error
```

## Builtin Authentication

### How It Works

1. User submits username and password
2. System looks up user by username in database
3. Password is verified against bcrypt hash
4. If match, authentication succeeds

### User Creation

Builtin users are created by admins through:
- Admin panel (Admin > Users)
- Initial admin via config file

### Password Storage

Passwords are hashed using bcrypt with a cost factor of 10. The original password is never stored.

## LDAP Authentication

### How It Works

1. User submits username and password
2. System binds to LDAP with service account
3. Searches for user with configured filter
4. Attempts bind with user's DN and password
5. If bind succeeds, extracts user attributes
6. Creates or updates local user record

### User Provisioning

LDAP users are automatically provisioned on first login:
- Username from `username_attr`
- Email from `email_attr`
- Display name from `display_name_attr`
- Auth source set to "ldap"

### Group Mapping

If group attributes are configured:
1. Groups are extracted from user's LDAP entry
2. Group mappings in database are checked
3. Matching projects are granted access

```yaml
project_groups:
  - group: "cn=developers,ou=groups,dc=example,dc=com"
    project: "api-docs"
    role: "editor"
```

## OAuth2 Authentication

### Flow

```
    User clicks "Login with SSO"
              │
              ▼
    Redirect to provider's auth_url
              │
              ▼
    User authenticates with provider
              │
              ▼
    Provider redirects to callback URL
              │
              ▼
    Exchange code for tokens at token_url
              │
              ▼
    Fetch user info from userinfo_url
              │
              ▼
    Create/update user, create session
```

### User Provisioning

OAuth2 users are provisioned from claims:
- Username from configured `username` claim
- Email from `email` claim
- Display name from `display_name` claim
- Auth source set to "oauth2"

### Group Mapping

If the `groups` claim is configured:
1. Groups are extracted from the ID token or userinfo
2. Group mappings in database are checked
3. Matching projects are granted access

## Session Management

### Session Creation

After successful authentication:
1. Generate random session ID (32 bytes, base64)
2. Store session record with user ID and expiry
3. Set HTTP cookie with session ID

### Session Validation

On each request:
1. Read session ID from cookie
2. Look up session in database
3. Check expiry
4. Load associated user
5. Add user to request context

### Session Expiry

Sessions expire after `auth.session.max_age` seconds (default: 24 hours). Expired sessions are cleaned up periodically.

## API Token Authentication

For programmatic access:

1. Request includes `Authorization: Bearer {token}` header
2. Token is SHA-256 hashed
3. Hash is looked up in database
4. Associated user is loaded
5. Request proceeds with user context

API tokens don't create sessions; each request is authenticated independently.

## Auth Source Tracking

Users have an `auth_source` field:
- `builtin`: Password stored locally
- `ldap`: Authenticated via LDAP
- `oauth2`: Authenticated via OAuth2
- `robot`: API-only user

This tracks how the user was created and prevents password operations on external users.

## Security Considerations

### Password Handling

- Passwords are never logged
- Bcrypt with cost factor 10
- Timing-safe comparison

### Session Security

- Random session IDs (256 bits of entropy)
- Secure cookie flag available
- Session fixation prevention (new ID on login)

### Rate Limiting

Login attempts are rate limited to prevent brute force attacks.

### Token Security

- Tokens are stored as SHA-256 hashes
- Plain token shown only once at creation
- No way to retrieve token from hash

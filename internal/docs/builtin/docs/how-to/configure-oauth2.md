# Configure OAuth2 Authentication

This guide shows you how to configure OAuth2/OIDC authentication for single sign-on.

## Overview

Asiakirjat supports OAuth2 authentication with any OIDC-compliant provider:

- Keycloak
- Okta
- Azure AD
- Google Workspace
- Auth0
- GitLab
- GitHub

## Basic Configuration

Add the following to your `config.yaml`:

```yaml
auth:
  oauth2:
    enabled: true
    provider: "generic"
    client_id: "asiakirjat"
    client_secret: "your-client-secret"
    auth_url: "https://idp.example.com/oauth/authorize"
    token_url: "https://idp.example.com/oauth/token"
    userinfo_url: "https://idp.example.com/oauth/userinfo"
    redirect_url: "https://docs.example.com/auth/callback"
    scopes:
      - openid
      - profile
      - email
```

## Configuration Options

| Option | Description |
|--------|-------------|
| `enabled` | Set to `true` to enable OAuth2 |
| `provider` | Provider type: `generic`, `keycloak`, `okta`, `azure` |
| `client_id` | OAuth2 client ID |
| `client_secret` | OAuth2 client secret |
| `auth_url` | Authorization endpoint URL |
| `token_url` | Token endpoint URL |
| `userinfo_url` | UserInfo endpoint URL |
| `redirect_url` | Callback URL (must match provider config) |
| `scopes` | OAuth2 scopes to request |

## Claim Mapping

Map provider claims to Asiakirjat user attributes:

```yaml
auth:
  oauth2:
    claims:
      username: "preferred_username"  # or "sub", "email"
      email: "email"
      display_name: "name"
      groups: "groups"                # for group-based access
```

## Provider-Specific Examples

### Keycloak

```yaml
auth:
  oauth2:
    enabled: true
    provider: "keycloak"
    client_id: "asiakirjat"
    client_secret: "xxx"
    auth_url: "https://keycloak.example.com/realms/main/protocol/openid-connect/auth"
    token_url: "https://keycloak.example.com/realms/main/protocol/openid-connect/token"
    userinfo_url: "https://keycloak.example.com/realms/main/protocol/openid-connect/userinfo"
    redirect_url: "https://docs.example.com/auth/callback"
    scopes:
      - openid
      - profile
      - email
```

### Azure AD

```yaml
auth:
  oauth2:
    enabled: true
    provider: "azure"
    client_id: "your-app-id"
    client_secret: "your-secret"
    auth_url: "https://login.microsoftonline.com/{tenant}/oauth2/v2.0/authorize"
    token_url: "https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token"
    userinfo_url: "https://graph.microsoft.com/oidc/userinfo"
    redirect_url: "https://docs.example.com/auth/callback"
    scopes:
      - openid
      - profile
      - email
    claims:
      username: "preferred_username"
      groups: "groups"
```

### Okta

```yaml
auth:
  oauth2:
    enabled: true
    provider: "okta"
    client_id: "your-client-id"
    client_secret: "your-secret"
    auth_url: "https://yourorg.okta.com/oauth2/default/v1/authorize"
    token_url: "https://yourorg.okta.com/oauth2/default/v1/token"
    userinfo_url: "https://yourorg.okta.com/oauth2/default/v1/userinfo"
    redirect_url: "https://docs.example.com/auth/callback"
    scopes:
      - openid
      - profile
      - email
      - groups
```

## Group-Based Project Access

Grant project access based on OAuth2 group claims:

```yaml
auth:
  oauth2:
    claims:
      groups: "groups"

    project_groups:
      - group: "documentation-team"
        project: "internal-docs"
        role: "editor"
      - group: "developers"
        project: "api-docs"
        role: "viewer"
```

## Login Button

When OAuth2 is enabled, a "Login with SSO" button appears on the login page. Users can still log in with built-in accounts if configured.

## Troubleshooting

**"Invalid redirect_uri"**
- Ensure redirect_url exactly matches the URI registered with your provider
- Check for trailing slashes

**"Invalid client credentials"**
- Verify client_id and client_secret
- Check if the secret has expired

**User attributes not populated**
- Check scopes include required permissions
- Verify claim mapping matches provider's claims

**Groups not working**
- Ensure `groups` scope is requested
- Verify the `claims.groups` mapping
- Check provider is configured to include groups in tokens

## Environment Variables

```bash
ASIAKIRJAT_AUTH_OAUTH2_ENABLED=true
ASIAKIRJAT_AUTH_OAUTH2_CLIENT_ID=asiakirjat
ASIAKIRJAT_AUTH_OAUTH2_CLIENT_SECRET=secret
```

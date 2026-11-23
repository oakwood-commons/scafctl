# Custom OAuth2 Auth Handler Examples

These examples show how to configure custom OAuth2 auth handlers for
registries and OAuth2 services that don't have built-in support.

## Quay.io (Robot Account)

```yaml
# ~/.config/scafctl/config.yaml
auth:
  customOAuth2:
    - name: quay
      displayName: "Quay.io"
      tokenURL: "https://quay.io/oauth/token"
      clientID: "your-quay-app-client-id"
      clientSecret: "your-quay-app-client-secret"
      defaultFlow: client_credentials
      scopes:
        - "repo:read"
      registry: "quay.io"
      registryUsername: "$oauthtoken"
```

## Quay.io / Quay-Style Registry (Interactive Browser Login)

For personal developer workstations where users authenticate as themselves
rather than with a shared robot account, configure `defaultFlow: interactive`
against the registry's OAuth2 authorization endpoint.

> [!IMPORTANT]
> **Prerequisite -- active browser login session:** Servers whose
> `authorizeURL` returns a bare `401 Unauthorized` response instead of
> redirecting unauthenticated browsers to a sign-in page (including Quay.io,
> self-hosted Quay-style registries, and similarly configured OAuth2 services)
> require an **active web-UI login session in the browser that opens the
> authorize URL** before you run `scafctl auth login <name>`.
>
> Sign in to `<registry-host>` in that browser first, then run
> `scafctl auth login <name>`. `scafctl` opens your system default browser
> automatically -- if your active session lives in a different browser or
> browser profile, copy the full `authorizeURL` from the opened tab's address
> bar into that authenticated browser. When automatic browser launch is
> unavailable (for example, headless WSL), pass `--debug` to log the
> `authorizeURL` in terminal output; over SSH, also forward `callbackPort`
> (for example, `ssh -L 8400:localhost:8400 ...`) so the browser redirect
> reaches `scafctl`'s localhost listener.

Quay.io and self-hosted Quay OAuth applications use the OAuth2 implicit grant
(`responseType: token`, which automatically disables PKCE; `tokenURL` is
optional when `responseType: token` is set):

```yaml
# ~/.config/scafctl/config.yaml
auth:
  customOAuth2:
    - name: quay
      displayName: "Quay.io"
      # For a self-hosted registry, replace quay.io with registry.example.com
      authorizeURL: "https://quay.io/oauth/authorize"
      clientID: "your-quay-app-client-id"
      defaultFlow: interactive
      responseType: token
      # Match the Redirect URI registered on the Quay OAuth application
      # (for example, http://localhost:8400)
      callbackPort: 8400
      scopes:
        - "repo:read"
      registry: "quay.io"
      registryUsername: "$oauthtoken"
      verifyURL: "https://quay.io/api/v1/user/"
      identityFields:
        username: "username"
        email: "email"
        name: "name"
```

For registries or OAuth2 servers that support the standard Authorization Code
flow (`responseType: code`, the default) while still requiring an existing
browser session on `authorizeURL`, include `tokenURL` and omit `responseType`
(set `disablePKCE: true` only if the server rejects PKCE `code_challenge`
parameters):

```yaml
auth:
  customOAuth2:
    - name: registry-sso
      displayName: "Container Registry"
      authorizeURL: "https://registry.example.com/oauth/authorize"
      tokenURL: "https://registry.example.com/oauth/token"
      clientID: "your-registry-app-client-id"
      defaultFlow: interactive
      callbackPort: 8400
      scopes:
        - "repo:read"
      registry: "registry.example.com"
      registryUsername: "$oauthtoken"
```

Authenticate after signing in to the registry's web UI in your browser:

```bash
# 1. Sign in to https://quay.io (or https://registry.example.com) in your browser first
# 2. Run interactive login (automatically bridges credentials to quay.io via 'registry')
scafctl auth login quay

# Verify handler identity and bridged registry credentials
scafctl auth status quay
scafctl auth status

# Optional: also write bridged credentials for Docker/Podman interop, or re-bridge explicitly
scafctl auth login quay --force --write-registry-auth
scafctl catalog login quay.io
```

## GitLab Container Registry

```yaml
auth:
  customOAuth2:
    - name: gitlab
      displayName: "GitLab Registry"
      tokenURL: "https://gitlab.com/oauth/token"
      authorizeURL: "https://gitlab.com/oauth/authorize"
      clientID: "your-gitlab-app-id"
      defaultFlow: interactive
      callbackPort: 9876
      scopes:
        - "read_registry"
        - "write_registry"
      registry: "registry.gitlab.com"
      registryUsername: "oauth2"
      verifyURL: "https://gitlab.com/api/v4/user"
      identityFields:
        username: "username"
        email: "email"
        name: "name"
```

## Harbor (OIDC Integration)

```yaml
auth:
  customOAuth2:
    - name: harbor
      displayName: "Harbor Registry"
      tokenURL: "https://harbor.example.com/service/token"
      authorizeURL: "https://harbor.example.com/c/oidc/login"
      clientID: "harbor-scafctl"
      defaultFlow: interactive
      callbackPort: 8085
      registry: "harbor.example.com"
      verifyURL: "https://harbor.example.com/api/v2.0/users/current"
      identityFields:
        username: "username"
        email: "email"
        name: "realname"
```

## Token Exchange (e.g., GCP Artifact Registry via custom IDP)

Some registries require a two-step auth: get an OAuth2 token, then
exchange it for a registry-specific token.

```yaml
auth:
  customOAuth2:
    - name: custom-ar
      displayName: "Custom Artifact Registry"
      tokenURL: "https://idp.example.com/oauth2/token"
      clientID: "ar-client"
      clientSecret: "ar-secret"
      defaultFlow: client_credentials
      scopes:
        - "registry:catalog:*"
      registry: "us-docker.pkg.dev"
      registryUsername: "oauth2accesstoken"
      tokenExchange:
        url: "https://us-docker.pkg.dev/v2/token?service=us-docker.pkg.dev"
        method: GET
        tokenJSONPath: "token"
```

## Non-Registry OAuth2 (API Authentication)

Custom handlers can also be used for OAuth2 services unrelated to OCI
registries.

```yaml
auth:
  customOAuth2:
    - name: my-api
      displayName: "Internal API"
      tokenURL: "https://auth.example.com/oauth2/token"
      deviceAuthURL: "https://auth.example.com/oauth2/device"
      clientID: "scafctl-cli"
      defaultFlow: device_code
      scopes:
        - "api:read"
        - "api:write"
      verifyURL: "https://auth.example.com/userinfo"
      identityFields:
        username: "preferred_username"
        email: "email"
        name: "name"
```

## Related

- [Authentication Tutorial](../../docs/tutorials/auth-tutorial.md) -- interactive login walkthroughs, custom OAuth2 handlers, and troubleshooting

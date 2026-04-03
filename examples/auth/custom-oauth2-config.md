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

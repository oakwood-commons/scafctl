# Custom OAuth2 Auth Handler Examples

These examples show how to configure custom OAuth2 auth handlers for
registries and OAuth2 services that don't have built-in support.

## Managing Handlers from the CLI (`scafctl auth oauth2`)

You can add, inspect, and remove `auth.customOAuth2[]` entries directly from
the CLI without editing `config.yaml` by hand:

```bash
# Add an interactive OAuth2 handler via CLI flags
scafctl auth oauth2 add quay \
  --display-name "Quay.io" \
  --authorize-url https://quay.io/oauth/authorize \
  --client-id your-quay-app-client-id \
  --default-flow interactive \
  --response-type token \
  --callback-port 8400 \
  --scopes repo:read \
  --registry quay.io \
  --registry-username '$oauthtoken'

# Add a client_credentials handler reading the secret from stdin
# (stored encrypted as secret://ci-service-client-secret)
printf '%s' "$CLIENT_SECRET" | scafctl auth oauth2 add ci-service \
  --display-name "CI Service" \
  --token-url https://idp.example.com/oauth2/token \
  --client-id ci-client \
  --default-flow client_credentials \
  --client-secret-stdin

# Load a handler with nested blocks (tokenExchange, dynamicClientRegistration, identityFields)
scafctl auth oauth2 add --from-file examples/auth/custom-oauth2-handler.yaml

# Update an existing custom OAuth2 entry in place
scafctl auth oauth2 add quay --from-file ./quay.yaml --force

# List configured custom OAuth2 handlers (secrets are redacted)
scafctl auth oauth2 list
scafctl auth oauth2 list -i
scafctl auth oauth2 list -o json

# Inspect all registered handlers (custom entries report source: custom-oauth2)
scafctl auth handlers

# Remove a custom OAuth2 handler from config.yaml
scafctl auth oauth2 remove ci-service
```

> [!NOTE]
> Plaintext secrets are rejected on `--client-secret`, `--federated-token`, and
> `--token` CLI flags so credentials never appear in process arguments or shell
> history. Pass a `SecretRef` URI (`secret://`, `env://`, or `file://`) or pipe
> a client secret via `--client-secret-stdin`.

When using `--from-file`, the file contains a single handler object (not wrapped
in `auth.customOAuth2` -- see [custom-oauth2-handler.yaml](custom-oauth2-handler.yaml)):

```yaml
name: custom-ar
displayName: "Custom Artifact Registry"
tokenURL: "https://idp.example.com/oauth2/token"
clientID: "ar-client"
clientSecret: "secret://ar-client-secret"
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

## Quay.io (Robot Account -- Zero-Ceremony Static Token)

For headless CI/CD pipelines or Kubernetes jobs using a pre-issued Quay robot
token, configure the `pat` flow with a `SecretRef` URI (`secret://`, `file://`,
or `env://`) and set `username` to the robot account name (`<org>+<robot>`).
`GetToken`, `Status`, and OCI registry credential bridging work immediately
with zero prior `scafctl auth login` step, and `username` overrides any
`$oauthtoken` `registryUsername` when `pat` is active:

```yaml
# ~/.config/scafctl/config.yaml
auth:
  customOAuth2:
    - name: quay
      displayName: "Quay.io Robot"
      defaultFlow: pat
      # SecretRef URI: secret://<name>, env://<VAR>, or file://<PATH>
      token: "secret://quay-robot-token"
      username: "myorg+ci_robot"
      registry: "quay.io"
      verifyURL: "https://quay.io/api/v1/user/"
      identityFields:
        username: "username"
```

Store the robot token once on workstations (or use `env://QUAY_ROBOT_TOKEN` /
`file:///var/run/secrets/...` in ephemeral CI runners):

```bash
# Store secret in scafctl's encrypted secret store
scafctl secrets set quay-robot-token

# Verify status or mint/bridge credentials with no prior 'auth login' step
scafctl auth status quay --exit-code
scafctl auth token quay --raw
scafctl catalog login quay.io
```

## Quay.io (OAuth2 Application Client Credentials)

```yaml
# ~/.config/scafctl/config.yaml
auth:
  customOAuth2:
    - name: quay
      displayName: "Quay.io"
      tokenURL: "https://quay.io/oauth/token"
      clientID: "your-quay-app-client-id"
      clientSecret: "secret://quay-app-client-secret"
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
> `scafctl auth login <name>`. If your active session lives in a non-default
> browser, pass `--browser <browser>` (for example,
> `scafctl auth login <name> --browser msedge`) or set `auth.browser` in
> `config.yaml` so `scafctl` opens that browser directly -- or copy the full
> `authorizeURL` from the opened tab's address bar into your authenticated
> browser. When no browser launcher can start (for example, `xdg-open` is not
> installed in a minimal container or headless Linux environment), `scafctl`
> prints the `authorizeURL` automatically so you can open it manually (if
> `xdg-open` starts and exits later without a display, pass `--debug` to inspect
> launcher behavior). Over SSH, also forward `callbackPort` (for example,
> `ssh -L 8400:localhost:8400 ...`) so the browser redirect reaches `scafctl`'s
> localhost listener.

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
scafctl auth status --all

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

## Workload Identity Federation (Kubernetes & GitHub Actions -- Zero-Ceremony CI)

Custom OAuth2 handlers support secretless Workload Identity Federation
(`workload_identity`) with zero prior `scafctl auth login` step. The federated
JWT (`federatedToken` or `federatedTokenFile`) is re-resolved and trimmed on
every token refresh so rotated Kubernetes projected volume tokens are picked up
automatically.

### RFC 7523 JWT Client Assertion (`client_credentials`, default)

Compatible with Keycloak, Okta, Auth0, GitLab, Ping, Ory, and standard OAuth2
authorization servers that accept `client_assertion_type=jwt-bearer`:

```yaml
auth:
  customOAuth2:
    - name: internal-idp
      displayName: "Internal OIDC Service"
      tokenURL: "https://idp.example.com/oauth2/token"
      clientID: "scafctl-k8s-workload"
      # Re-read automatically whenever a cached access token expires
      federatedToken: "file:///var/run/secrets/tokens/idp-token"
      audience: "api://internal-service"
      defaultFlow: workload_identity
      scopes:
        - "service:read"
```

For GitHub Actions OIDC tokens exposed via environment variables:

```yaml
auth:
  customOAuth2:
    - name: ci-api
      displayName: "CI Deployment API"
      tokenURL: "https://idp.example.com/oauth2/token"
      clientID: "github-actions-deployer"
      federatedToken: "env://ACTIONS_ID_TOKEN"
      workloadIdentityGrantType: client_credentials
      defaultFlow: workload_identity
      scopes:
        - "deploy:write"
```

### RFC 8693 OAuth 2.0 Token Exchange (`token_exchange`)

Compatible with HashiCorp Vault, Dex, and STS-style token brokers (`clientID`
is optional; if both `clientID` and `clientSecret` are set, HTTP Basic
authentication is sent on the token exchange request):

```yaml
auth:
  customOAuth2:
    - name: sts-broker
      displayName: "STS Token Broker"
      tokenURL: "https://sts.example.com/v1/oauth/token"
      federatedTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token"
      workloadIdentityGrantType: token_exchange
      subjectTokenType: "urn:ietf:params:oauth:token-type:jwt"
      audience: "https://api.example.com"
      defaultFlow: workload_identity
      scopes:
        - "api:read"
```

## Hybrid Workstation + Headless Pipeline Handler

A single handler can serve interactive browser login on developer workstations
while falling back automatically (`workload_identity` -> `client_credentials`
-> `pat`) in headless CI containers when no interactive login session exists:

```yaml
auth:
  customOAuth2:
    - name: registry-auth
      displayName: "Container Registry"
      authorizeURL: "https://registry.example.com/oauth/authorize"
      tokenURL: "https://registry.example.com/oauth/token"
      clientID: "registry-cli"
      # Used on developer workstations when running 'scafctl auth login registry-auth'
      defaultFlow: interactive
      callbackPort: 8400
      registry: "registry.example.com"
      registryUsername: "$oauthtoken"
      # Used automatically in CI when REGISTRY_ROBOT_TOKEN is set (overrides registryUsername)
      token: "env://REGISTRY_ROBOT_TOKEN"
      username: "acme+ci_robot"
```

## SecretRef Credential Syntax

All custom OAuth2 credential fields support `SecretRef` URIs so plaintext
secrets never appear in `config.yaml`:

| Scheme | Example | Source |
| --- | --- | --- |
| `secret://<name>` | `secret://quay-robot-token` | Encrypted `scafctl` secret store (`scafctl secrets set <name>`) |
| `file://<path>` | `file:///var/run/secrets/tokens/token` | Local or projected volume file (trimmed of surrounding whitespace) |
| `env://<VAR>` | `env://CI_OIDC_TOKEN` | Process environment variable |

`federatedToken` and `token` require a `SecretRef` URI; `clientSecret`,
`username`, and `dynamicClientRegistration.initialAccessToken` accept either a
`SecretRef` URI or a literal string. Internal `scafctl.*` secret keys are
reserved and rejected by `secret://`.

## Related

- [Authentication Tutorial](../../docs/tutorials/auth-tutorial.md) -- interactive login walkthroughs, custom OAuth2 handlers, and troubleshooting

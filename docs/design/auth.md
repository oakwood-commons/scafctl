---
title: "Authentication"
weight: 5
---

# Authentication and Authorization

## Purpose

Authentication in scafctl is declarative, provider-driven, and execution-agnostic.

Providers declare what kind of token they require. scafctl or an external executor decides how that token is obtained and supplied. For local users, scafctl can initiate interactive authentication flows. For APIs and workflow engines, credentials are injected explicitly.

Authentication is a system concern, not a provider implementation detail.

---

## Terminology

- **Auth Handler**: A component that implements the `auth.Handler` interface and manages identity verification, credential storage, and token acquisition for a specific identity provider (e.g., Entra, GitHub). Auth handlers are registered in the auth registry.
- **Auth Provider** (in provider inputs): The `authProvider` field in HTTP provider inputs that specifies which auth handler to use for a request.
- **Provider**: Action/resolver providers that perform work (e.g., HTTP, shell, file). Distinct from auth handlers.
- **Auth Handler Artifact**: A go-plugin binary distributed via the catalog that exposes one or more auth handlers.

---

## Built-in vs External Auth Handlers

scafctl provides built-in auth handlers for common identity providers:

| Handler | Status | Description |
|---------|--------|-------------|
| `entra` | ✅ Implemented | Microsoft Entra ID (Azure AD) |
| `github` | ✅ Implemented | GitHub OAuth (Device Code + PAT) |
| `gcp` | ✅ Implemented | Google Cloud Platform (ADC, Service Account, Metadata, Workload Identity, gcloud ADC, Impersonation) |

**External Auth Handlers** can be distributed via the catalog for custom identity providers:

```bash
# Push an auth handler to the catalog
scafctl catalog push okta-handler@1.0.0 --catalog ghcr.io/myorg

# Pull an auth handler
scafctl catalog pull ghcr.io/myorg/auth-handlers/okta-handler@1.0.0

# The handler is then available for use
scafctl auth login okta
```

External auth handlers use the same go-plugin mechanism as providers. See [Plugins](plugins.md) for architecture details.

---

## Core Principles

- Providers declare auth requirements, never credentials
- Token acquisition is separated from provider execution
- Long-lived credentials are managed centrally
- Execution tokens are short-lived and ephemeral
- Render mode never acquires credentials
- Raw secrets and tokens never appear in solution files

---

## Auth Handlers

Auth handlers define how identities are authenticated and how tokens are minted.

Examples:

- `entra`
- `github`
- `gcloud`

Auth handlers are responsible for:

- Supporting one or more authentication flows
- Managing refresh tokens or equivalent credentials
- Minting execution tokens for providers
- Normalizing token metadata and claims
- Declaring their capabilities for CLI flag validation

Auth handlers are not action providers and do not perform side effects outside authentication.

### Handler Capabilities

Each handler declares a set of capabilities that describe which features it supports. CLI commands use these capabilities to dynamically validate flags and provide meaningful errors.

| Capability | Description | Entra | GitHub | GCP |
|------------|-------------|-------|--------|-----|
| `scopes_on_login` | Supports specifying scopes at login time | ✅ | ✅ | ✅ |
| `scopes_on_token_request` | Supports per-request scopes when acquiring tokens | ✅ | ❌ | ✅ |
| `tenant_id` | Supports tenant ID parameter | ✅ | ❌ | ❌ |
| `hostname` | Supports hostname parameter (enterprise/self-hosted) | ❌ | ✅ | ❌ |
| `federated_token` | Supports federated token input (workload identity) | ✅ | ❌ | ✅ |
| `callback_port` | Supports `--callback-port` for fixed OAuth redirect URI | ✅ | ❌ | ✅ |

**Why capabilities matter**: GitHub's OAuth does not support changing scopes on token refresh — scopes are fixed at login time. Entra ID supports requesting different resource scopes per token request. Instead of hardcoding these differences in CLI commands, each handler declares its capabilities, and the CLI validates flags accordingly.

This design makes plugin-loaded auth handlers work without CLI code changes — a plugin handler declares its capabilities, and the CLI dynamically adapts.

**Example**: Running `scafctl auth token github --scope repo` returns an error:
> the "github" auth handler does not support per-request scopes; scopes are fixed at login time. Use 'scafctl auth login github --scope <scope>' to change scopes

### Handler Registry

Auth handlers are managed via a thread-safe registry (`auth.Registry`). CLI commands look up handlers by name from the registry in context, rather than using hardcoded switch statements. This supports:

- Built-in handlers registered at startup
- Plugin-loaded handlers registered after discovery
- Dynamic handler enumeration for commands like `auth list` and `auth status` (shows all registered handlers)

---

## CLI Authentication

Local users authenticate explicitly using the `auth` command.

### Device Code Flow (Interactive)

~~~bash
scafctl auth login entra
~~~

Behavior:

- Initiates device code flow
- User authenticates in a browser
- A refresh token (or equivalent credential) is obtained
- Credentials are stored securely by scafctl
- No provider execution occurs

This establishes a local identity context for future runs.

### Authorization Code + PKCE Flow (Default Interactive)

The default interactive flow uses OAuth 2.0 Authorization Code with PKCE. It opens a browser to the Entra authorize endpoint and listens on a local HTTP server for the redirect callback:

~~~bash
# Default — uses ephemeral port
scafctl auth login entra

# Fixed port — for app registrations with specific redirect URIs
scafctl auth login entra --callback-port 8400
~~~

Behavior:

- Starts a local HTTP callback server on `localhost` (ephemeral or fixed port)
- Generates PKCE code verifier and challenge
- Opens the browser to the Entra `/authorize` endpoint
- Receives the authorization code via redirect, exchanges it for tokens
- Stores refresh token and metadata in the secret store

The `--callback-port` flag binds the callback server to a specific port so the redirect URI is predictable (e.g., `http://localhost:8400`). This is necessary when the app registration only allows specific redirect URIs. When omitted, the OS assigns an ephemeral port.

**AADSTS500113 handling**: When the redirect URI is not registered on the app, Entra shows an error in the browser but never redirects to the callback server. The CLI detects this scenario by providing an informative timeout message that suggests registering `http://localhost` as a redirect URI or using `--flow device-code`.

### Service Principal Flow (Non-Interactive)

For CI/CD and automation scenarios, use service principal authentication:

~~~bash
# Set credentials in environment
export AZURE_CLIENT_ID="..."
export AZURE_TENANT_ID="..."
export AZURE_CLIENT_SECRET="..."

# Auto-detects service principal from env vars
scafctl auth login entra

# Or explicitly specify the flow
scafctl auth login entra --flow service-principal
~~~

Behavior:

- Reads credentials from environment variables
- Acquires token using OAuth2 client_credentials grant
- Validates credentials on login
- No user interaction required
- Tokens are cached like device code flow

**Supported Flows:**

| Flow | Flag | Use Case |
|------|------|----------|
| Device Code | `--flow device-code` | Interactive user authentication |
| Service Principal | `--flow service-principal` | CI/CD pipelines, automation |
| Workload Identity | `--flow workload-identity` | Kubernetes (AKS) workload identity |

### GitHub Device Code Flow

~~~bash
scafctl auth login github
~~~

Behavior:

- Initiates GitHub OAuth device code flow
- User authenticates in a browser at https://github.com/login/device
- An access token (and optionally refresh token) is obtained
- Credentials are stored securely
- Supports GitHub Enterprise Server via `--hostname`

### GitHub PAT Flow (CI/CD)

~~~bash
# Set token in environment
export GITHUB_TOKEN="ghp_..."

# Auto-detects PAT from env vars
scafctl auth login github

# Or explicitly specify the flow
scafctl auth login github --flow pat
~~~

Behavior:

- Reads `GITHUB_TOKEN` or `GH_TOKEN` from environment
- Validates the token by calling the GitHub API
- No user interaction required
- Token is cached for performance

### Workload Identity Flow (Kubernetes)

For Kubernetes workloads running in AKS with workload identity enabled:

~~~bash
# Environment variables are automatically set by Kubernetes
# AZURE_CLIENT_ID, AZURE_TENANT_ID, and AZURE_FEDERATED_TOKEN_FILE

# Auto-detects workload identity from projected token file
scafctl auth login entra

# Or explicitly specify the flow
scafctl auth login entra --flow workload-identity

# For testing, pass a federated token directly
scafctl auth login entra --flow workload-identity --federated-token "eyJ..."
~~~

Behavior:

- Reads federated token from projected service account token file
- Exchanges token using OAuth2 client assertion grant
- No user interaction required
- Tokens are cached like other flows
- **Highest priority**: takes precedence over all other flows — stored refresh tokens (device code) and service principal credentials are bypassed when WIF env vars are present

### Flow Priority

When `GetToken` is called, the Entra handler selects the flow in this order:

| Priority | Flow | Condition |
|----------|------|-----------|
| 1 | Workload Identity | `AZURE_FEDERATED_TOKEN_FILE` or `AZURE_FEDERATED_TOKEN` is set and valid |
| 2 | Service Principal | `AZURE_CLIENT_SECRET` is set |
| 3 | Device Code (refresh token) | A refresh token is present in the system secret store |

Only the first matching flow is used.

### Isolation from Stored Refresh Tokens

WIF and the device-code refresh token are **completely independent** and stored separately:

- WIF reads only environment variables and the projected token file — it never reads, writes, or modifies `scafctl.auth.entra.refresh_token`
- Running `scafctl auth login entra` with WIF env vars present does **not** clear or replace any stored refresh token
- A refresh token from a prior device-code login may silently coexist in the secret store while WIF is active; `scafctl auth list` will display both

### Fallback Behavior

If WIF env vars are later removed or the token file disappears, the handler automatically falls through to the next available flow (service principal, then stored refresh token). No reconfiguration is required.

This is useful in migration scenarios, for example when bootstrapping WIF on a cluster: a developer's device-code session is still usable outside the cluster without any changes.

### Stale Stored Credentials

Because WIF never clears the secret store, a refresh token from a previous device-code login may remain stored after WIF is deployed. While this token is never used while WIF is active, it still appears in `scafctl auth list` and counts against the 90-day idle expiry.

If you want a clean state after switching to WIF, explicitly clear stored credentials:

~~~bash
scafctl auth logout entra
~~~

This removes the refresh token and access token cache without affecting WIF, which is entirely env-var driven.

---

## Refresh Token Rotation

Entra ID issues a **new refresh token value** on every use of an existing refresh token (this is called rolling or rotating refresh tokens). Key points:

- The **lifetime** of a refresh token is 90 days, measured as a **sliding window** — each successful use resets the 90-day clock
- The old token value is invalidated and the new value is atomically stored in the secret store
- This rotation is transparent to the user; from scafctl's perspective the session simply continues
- A refresh token that has not been used for 90 consecutive days will expire and require re-authentication

Scafctl handles rotation automatically in `mintToken()`: if the token response contains a new refresh token value, it is stored immediately before the access token is returned.

---

## Credential Storage

Auth handlers manage credential storage using the `pkg/secrets` package, which provides a cross-platform secure storage abstraction.

Rules:

- Refresh tokens are stored securely (device code flow)
- Service principal credentials are read from environment variables only
- Storage is handler-specific
- Tokens are scoped to the handler and tenant
- Credentials are never embedded in solutions
- Credentials are never exposed to providers directly

### Device Code Flow Storage (Entra)

Uses the system secret store for long-lived credentials.

### Device Code Flow Storage (GitHub)

Uses the system secret store for access tokens and optional refresh tokens.

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub personal access token or Actions token |
| `GH_TOKEN` | GitHub personal access token (gh CLI convention) |
| `GH_HOST` | GitHub hostname for Enterprise Server |

### Service Principal Flow Storage (Entra)

Credentials are read from environment variables (never stored):

| Variable | Description |
|----------|-------------|
| `AZURE_CLIENT_ID` | Application (client) ID |
| `AZURE_TENANT_ID` | Directory (tenant) ID |
| `AZURE_CLIENT_SECRET` | Client secret value |

Access tokens are still cached to disk like device code flow.

### Workload Identity Flow Storage

Credentials are read from environment variables and projected token files (never stored):

| Variable | Description |
|----------|-------------|
| `AZURE_CLIENT_ID` | Application (client) ID |
| `AZURE_TENANT_ID` | Directory (tenant) ID |
| `AZURE_FEDERATED_TOKEN_FILE` | Path to projected service account token file |
| `AZURE_FEDERATED_TOKEN` | Raw federated token (for testing, takes precedence over file) |
| `AZURE_AUTHORITY_HOST` | Azure AD authority host (optional, defaults to `https://login.microsoftonline.com`) |

The federated token file is read fresh on each request as Kubernetes rotates the file.
Access tokens are cached to disk like other flows.

### Secret Naming Convention

Secrets are stored using a hierarchical naming scheme:

~~~
scafctl.auth.<handler>.<type>
~~~

For the Entra handler:

| Secret Name | Description |
|-------------|-------------|
| `scafctl.auth.entra.refresh_token` | Long-lived refresh token |
| `scafctl.auth.entra.metadata` | Token metadata (claims, tenant, client ID, expiry) |
| `scafctl.auth.entra.token.<flow>.<fingerprint>.<scope-hash>` | Cached access tokens partitioned by flow, config identity, and scope |

For the GitHub handler:

| Secret Name | Description |
|-------------|-------------|
| `scafctl.auth.github.refresh_token` | OAuth refresh token (if token expiration is enabled) |
| `scafctl.auth.github.access_token` | OAuth access token (non-expiring apps) |
| `scafctl.auth.github.metadata` | Token metadata (claims, hostname, client ID, expiry) |
| `scafctl.auth.github.token.<flow>.<fingerprint>.<scope-hash>` | Cached access tokens partitioned by flow, config identity, and scope |

The cache key encodes the authentication flow (e.g., `device_code`, `workload_identity`, `service_principal`), a config identity fingerprint, and the scope. The fingerprint is a truncated SHA-256 hash of the core identity fields for the current configuration (e.g., `clientID:tenantID` for Entra, `hostname` for GitHub). The scope hash is a base64url-encoded representation of the scope string. This three-segment key prevents cross-flow cache contamination — a token acquired via one authentication flow will never be served when a different flow is active — and prevents cross-config contamination — switching configurations (e.g., different tenant IDs, client IDs, or WIF audiences) results in a cache miss rather than serving stale tokens from the previous configuration.

The metadata includes the `clientId` used during login so that token refreshes always use the same client ID that originally obtained the refresh token, regardless of what client ID is in the current configuration.

---

## Token Caching

Access tokens are cached to disk (encrypted) for performance across CLI invocations.

### Cache Strategy

1. When a token is requested, check the disk cache first
2. If a cached token exists and has sufficient remaining validity, return it
3. Otherwise, use the refresh token to acquire a new access token
4. Cache the new access token for future requests

### MinValidFor

The `MinValidFor` parameter ensures tokens remain valid for the expected duration of the operation:

~~~go
type TokenOptions struct {
    Scope        string
    MinValidFor  time.Duration  // Minimum remaining validity required
    ForceRefresh bool           // Bypass cache and get fresh token
}
~~~

For HTTP provider requests, `MinValidFor` is calculated as:

~~~
MinValidFor = request_timeout + 60 seconds
~~~

This ensures the token won't expire during the request.

---

## Token Debugging

The `auth token` command allows retrieving access tokens for debugging:

~~~bash
scafctl auth token entra --scope "https://graph.microsoft.com/.default"
~~~

Features:

- Tokens are masked in table output for security
- Use `-o json` to get the full token
- Supports `--min-valid-for` to request tokens with specific validity
- Useful for testing API access with external tools (curl, httpie)

---

## Token Acquisition Model

### Execution Tokens

When a provider requires authentication:

1. The provider declares required auth type and scopes
2. scafctl selects the matching auth provider
3. The auth provider mints a short-lived execution token
4. The token is injected into provider execution
5. The token is discarded after use

Providers never see refresh tokens.

---

### Example Provider Declaration

~~~yaml
caasByID:
  authProvider: entra
  authScope: "{{ .platform.scope }}"
  headers:
    Accept: application/json, application/problem+json
  method: GET
  uri: https://{{ .platform.host }}/platform-assets/api/v1/kubenamespace/find?clientID={{ .platformClientID }}
~~~

Meaning:

- The provider requires an Entra-issued access token
- The token must include the declared scope
- The provider does not care how the token is obtained

### Automatic 401 Retry

When the HTTP provider receives a 401 Unauthorized response and auth is configured:

1. The provider requests a fresh token with `ForceRefresh: true`
2. The fresh token bypasses the cache
3. The request is retried once with the new token
4. If still 401, the response is returned to the caller

This handles cases where:

- A cached token was revoked server-side
- Token permissions were changed
- The token was invalidated for security reasons

---

## Render Mode Behavior

When running in render mode:

~~~bash
scafctl render solution myapp
~~~

- No authentication flows are initiated
- No tokens are minted
- Auth requirements are emitted declaratively
- External executors are responsible for supplying tokens

Rendered output includes auth requirements but no credentials.

---

## External Executors

When an external system executes a rendered graph:

- It must supply tokens matching declared auth requirements
- Tokens are injected as execution inputs
- scafctl does not manage credentials in this mode

This allows integration with:

- CI systems
- Workflow engines
- Platform-native identity systems

---

## Flow Summary

### Local CLI Execution

1. User logs in via `scafctl auth login`
2. Refresh token is stored securely
3. Provider declares auth requirements
4. Auth provider mints execution token
5. Provider executes with injected token

---

### Render and External Execution

1. Resolvers are executed
2. Actions are rendered
3. Auth requirements are emitted
4. External executor supplies tokens
5. Providers execute outside scafctl

---

## Design Constraints

- Providers must never acquire credentials
- Auth providers must manage refresh tokens
- Execution tokens must be short-lived
- Render mode must not initiate auth
- Secrets must never appear in solution artifacts

---

## Why This Model Works

This design:

- Matches cloud-native auth patterns
- Supports human and machine execution
- Avoids secret sprawl
- Enables portable execution graphs
- Keeps providers simple and auditable

---

## Future Enhancements

### Auth Claims Provider

A dedicated provider will be created to expose authentication claims (tenant ID, subject, scopes, etc.) for use in expressions and conditions. This enables conditional logic based on the authenticated identity without exposing raw tokens or secrets.

Example usage (proposed):

~~~yaml
inputs:
  tenantId:
    provider: auth
    inputs:
      handler: entra
      claim: tenantId
~~~

---

## Summary

Authentication in scafctl is explicit, declarative, and provider-driven. Auth providers manage identity and token minting. Providers declare required token types and scopes. scafctl supports interactive login for local users and clean integration with external executors, while keeping secrets out of configuration and artifacts.

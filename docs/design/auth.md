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

---

## Core Principles

- Providers declare auth requirements, never credentials
- Token acquisition is separated from provider execution
- Long-lived credentials are managed centrally
- Execution tokens are short-lived and ephemeral
- Render mode never acquires credentials
- Raw secrets and tokens never appear in solution files

---

## Auth Providers

Auth providers define how identities are authenticated and how tokens are minted.

Examples:

- `entra`
- `github`
- `gcloud`

Auth providers are responsible for:

- Supporting one or more authentication flows
- Managing refresh tokens or equivalent credentials
- Minting execution tokens for providers
- Normalizing token metadata and claims

Auth providers are not action providers and do not perform side effects outside authentication.

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
- Highest priority: takes precedence over service principal if both are configured

Logout clears stored credentials:

~~~bash
scafctl auth logout entra
~~~

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

### Device Code Flow Storage

Uses the system secret store for long-lived credentials.

### Service Principal Flow Storage

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
| `scafctl.auth.entra.metadata` | Token metadata (claims, tenant, expiry) |
| `scafctl.auth.entra.token.<scope-hash>` | Cached access tokens by scope |

The scope hash is a base64url-encoded representation of the scope string.

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

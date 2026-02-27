---
title: "GitHub Auth Handler"
weight: 6
---

# GitHub Auth Handler Implementation Plan

## Overview

Implement a builtin GitHub auth handler (`github`) following the established patterns from the Entra handler. The handler supports four authentication flows:

1. **Interactive (OAuth Authorization Code + PKCE)** — Browser-based login for local development (default)
2. **Device Code** — Headless/SSH fallback using OAuth device authorization grant
3. **PAT** — Personal Access Token from environment variables for CI/CD
4. **GitHub App** — Installation token for service-to-service automation

---

## Design Decisions

### Authentication Flows

| Flow | Use Case | Mechanism |
|------|----------|-----------|
| **Interactive** | Local development (default) | OAuth 2.0 Authorization Code + PKCE via browser redirect |
| **Device Code** | Headless / SSH environments | OAuth 2.0 device authorization grant via GitHub OAuth App |
| **PAT** | CI/CD pipelines, automation | Read `GITHUB_TOKEN` or `GH_TOKEN` from environment variables |
| **GitHub App** | Service-to-service automation | JWT → installation access token via GitHub App credentials |

**Rationale**: Interactive (browser) flow is the modern standard for CLI tools (`gh`, `az`, `gcloud` all use it as their default). Device code is the fallback for headless environments. PAT from environment mirrors the Entra handler's service principal pattern and aligns with GitHub Actions' `GITHUB_TOKEN` injection. GitHub App flow enables automated workflows that need repository access without a user context.

### OAuth Authorization Code + PKCE Flow

**Decision**: Use PKCE (Proof Key for Code Exchange) with the authorization code flow as the default interactive login.

**Rationale**: PKCE eliminates the need for a client secret in public clients (CLI apps). This is the same approach used by the Entra and GCP handlers. The flow opens the user's browser, handles the OAuth callback on a local HTTP server, and exchanges the authorization code for tokens.

**Sequence:**
1. Generate PKCE code verifier and challenge (S256)
2. Start local callback server on configured or ephemeral port
3. Open browser to `https://github.com/login/oauth/authorize` with `code_challenge`
4. User authorizes in browser → GitHub redirects to local server with `code`
5. Exchange code at `POST /login/oauth/access_token` with `code_verifier`
6. Fetch user claims via `GET /user`

### GitHub App Installation Token Flow

**Decision**: Support GitHub App authentication using a private key to mint JWTs and exchange them for installation access tokens.

**Rationale**: GitHub Apps are the recommended mechanism for service-to-service and automation scenarios. They provide fine-grained permissions, don't consume a user seat, and have built-in rate limit increases. This is analogous to Entra's service principal flow.

**Configuration sources** (in priority order):
- Config file fields: `appId`, `installationId`, `privateKey` / `privateKeyPath` / `privateKeySecretName`
- Environment variables: `SCAFCTL_GITHUB_APP_ID`, `SCAFCTL_GITHUB_APP_INSTALLATION_ID`, `SCAFCTL_GITHUB_APP_PRIVATE_KEY`, `SCAFCTL_GITHUB_APP_PRIVATE_KEY_PATH`
- Secret store: private key retrieved by `privateKeySecretName`

**Sequence:**
1. Load private key from inline PEM, file path, or secret store
2. Create RS256 JWT with `iss` = app ID, `iat` = now-60s, `exp` = now+10min
3. Validate JWT via `GET /app` (verifies app exists and key is correct)
4. Exchange JWT for installation token via `POST /app/installations/{id}/access_tokens`
5. Cache token until expiry, store metadata as service-principal identity

### OAuth App Client ID

**Decision**: Ship with a default public OAuth App client ID, allow override via `--client-id` flag and config.

**Rationale**: This matches industry practice:
- `gh` CLI ships with a hardcoded OAuth App client ID
- Azure CLI ships with its public client ID
- `gcloud` ships with a default client ID

Requiring users to register their own OAuth App before login creates unacceptable friction.

**Setup**: Create a GitHub OAuth App for scafctl with device flow enabled. The client ID will be hardcoded as the default in `config.go`.

### Default Scopes

**Decision**: Default to `gist`, `read:org`, `repo`, and `workflow` (matching the `gh` CLI).

| Scope | Purpose |
|-------|--------|
| `gist` | Create and manage gists |
| `read:org` | Read organization membership and teams |
| `repo` | Access repositories (catalog, solutions, templates) |
| `workflow` | Update GitHub Actions workflows |

**Rationale**: Matching the `gh` CLI defaults ensures a consistent experience and avoids permission gaps when using scafctl alongside `gh`. Users can customize via `--scope` at login time.

> **Note:** GitHub's OAuth token refresh endpoint does not accept a `scope`
> parameter — scopes are fixed at login time. The GitHub handler therefore
> declares `CapScopesOnLogin` but NOT `CapScopesOnTokenRequest`. Running
> `scafctl auth token github --scope ...` will return an error. See the
> [auth design doc](auth.md#handler-capabilities) for more on capabilities.

### Token Storage

**Decision**: Support refresh token rotation (OAuth token expiration + refresh tokens).

**Rationale**: GitHub OAuth Apps can optionally enable token expiration with refresh tokens. This is the modern best practice and matches the Entra handler pattern. If the OAuth App does not have token expiration enabled, the handler gracefully falls back to long-lived access tokens.

### GitHub Enterprise Server (GHES)

**Decision**: Support custom hostnames via `--hostname` flag and config.

**Rationale**: Many organizations use GHES. The handler should allow configuring a custom hostname that changes the OAuth and API base URLs:
- OAuth endpoints: `https://<hostname>/login/device/code`, `https://<hostname>/login/oauth/access_token`
- API endpoint: `https://<hostname>/api/v3/user`

Default hostname is `github.com`.

---

## GitHub OAuth Endpoints

| Endpoint | URL |
|----------|-----|
| Authorization (browser) | `GET https://github.com/login/oauth/authorize` |
| Device code request | `POST https://github.com/login/device/code` |
| Token exchange | `POST https://github.com/login/oauth/access_token` |
| Token refresh | `POST https://github.com/login/oauth/access_token` (with `grant_type=refresh_token`) |
| User info (claims) | `GET https://api.github.com/user` |
| App info (JWT validation) | `GET https://api.github.com/app` |
| Installation token | `POST https://api.github.com/app/installations/{id}/access_tokens` |

All endpoints accept and return JSON when `Accept: application/json` is set.

### Authorization Code + PKCE Flow Sequence

```
1. Generate PKCE code_verifier (random 32 bytes, base64url) and code_challenge (SHA-256)

2. Start local HTTP callback server on ephemeral or configured port

3. Open browser to:
   GET /login/oauth/authorize
     ?client_id=<id>
     &redirect_uri=http://localhost:<port>
     &scope=gist read:org repo workflow
     &state=<random>
     &code_challenge=<challenge>
     &code_challenge_method=S256

4. User authorizes → GitHub redirects to http://localhost:<port>?code=<code>&state=<state>

5. POST /login/oauth/access_token
   Body: client_id=<id>&code=<code>&redirect_uri=<uri>&code_verifier=<verifier>
   Response: { access_token, token_type, scope, refresh_token?, refresh_token_expires_in? }

6. GET /user with Authorization: Bearer <access_token>
   Extract claims: login, name, email, id
```

### Device Code Flow Sequence

```
1. POST /login/device/code
   Body: client_id=<id>&scope=gist read:org repo workflow
   Response: { device_code, user_code, verification_uri, expires_in, interval }

2. Display: "Enter code ABCD-1234 at https://github.com/login/device"

3. Poll POST /login/oauth/access_token
   Body: client_id=<id>&device_code=<code>&grant_type=urn:ietf:params:oauth:grant-type:device_code
   Until: access_token is returned (or error/timeout)

4. Response: { access_token, token_type, scope, refresh_token?, refresh_token_expires_in? }

5. GET /user with Authorization: Bearer <access_token>
   Extract claims: login, name, email, id
```

### PAT Flow Sequence

```
1. Read GITHUB_TOKEN or GH_TOKEN from environment
2. GET /user with Authorization: Bearer <token>
3. Extract claims: login, name, email, id
4. Validate token is functional
```

### GitHub App Flow Sequence

```
1. Load private key (inline PEM → file path → secret store → env var)
2. Create RS256 JWT:
   Header: { "alg": "RS256", "typ": "JWT" }
   Payload: { "iat": now-60, "exp": now+600, "iss": "<app_id>" }

3. GET /app with Authorization: Bearer <jwt>
   Validates that the App exists and the key is correct
   Response: { id, slug, name, ... }

4. POST /app/installations/<installation_id>/access_tokens
   Authorization: Bearer <jwt>
   Response: { token, expires_at, permissions, ... }

5. Cache token until expires_at
6. Store metadata with identity_type: service-principal
```

---

## File Structure

```
pkg/auth/github/
├── app_flow.go           # GitHub App installation token flow
├── app_flow_test.go
├── authcode_flow.go      # OAuth Authorization Code + PKCE flow
├── authcode_flow_test.go
├── cache.go              # Token caching (reuse Entra pattern)
├── cache_test.go
├── claims.go             # GitHub user → auth.Claims mapping
├── claims_test.go
├── config.go             # Config struct, defaults, validation, App fields
├── config_test.go
├── device_flow.go        # Device code OAuth flow
├── device_flow_test.go
├── handler.go            # Main handler implementing auth.Handler
├── handler_test.go
├── http.go               # HTTP client interface for testability
├── http_test.go
├── mock.go               # Test mocks
├── pat.go                # PAT flow (env var based)
├── pat_test.go
└── token.go              # Token response types and helpers
```

---

## Implementation Tasks

> **Status**: All phases complete. Implementation merged.

### Phase 1: Core Handler (Tasks 1-6) — ✅ Complete

| # | Task | Files | Status |
|---|------|-------|--------|
| 1 | Create handler skeleton | `config.go`, `handler.go` | ✅ |
| 2 | Implement HTTP client abstraction | `http.go` | ✅ |
| 3 | Implement device code flow | `device_flow.go` | ✅ |
| 4 | Implement PAT flow | `pat.go` | ✅ |
| 5 | Implement token cache | `cache.go`, `token.go` | ✅ |
| 6 | Implement claims extraction | `claims.go` | ✅ |

### Phase 2: Additional Flows (Tasks 7-8) — ✅ Complete

| # | Task | Files | Status |
|---|------|-------|--------|
| 7 | Implement OAuth Auth Code + PKCE flow | `authcode_flow.go` | ✅ |
| 8 | Implement GitHub App installation token flow | `app_flow.go` | ✅ |

### Phase 3: Wiring (Tasks 9-13) — ✅ Complete

| # | Task | Files | Status |
|---|------|-------|--------|
| 9 | Add `FlowPAT` and `FlowGitHubApp` flow constants | `pkg/auth/handler.go`, `pkg/auth/flow.go` | ✅ |
| 10 | Add GitHub config to global config (incl. App fields) | `pkg/config/types.go` | ✅ |
| 11 | Wire into root command | `pkg/cmd/scafctl/root.go` | ✅ |
| 12 | Wire into CLI auth commands | `pkg/cmd/scafctl/auth/handler.go` | ✅ |
| 13 | Update login command (default → interactive) | `pkg/cmd/scafctl/auth/login.go` | ✅ |

### Phase 4: Testing (Tasks 14-15) — ✅ Complete

| # | Task | Files | Status |
|---|------|-------|--------|
| 14 | Write unit tests | `pkg/auth/github/*_test.go` | ✅ (50+ tests) |
| 15 | Write CLI integration tests | `tests/integration/cli_test.go` | ✅ |

### Phase 5: Documentation (Tasks 16-18) — ✅ Complete

| # | Task | Files | Status |
|---|------|-------|--------|
| 16 | Update auth design doc | `docs/design/github-auth-handler.md` | ✅ |
| 17 | Update auth tutorial | `docs/tutorials/auth-tutorial.md` | ✅ |
| 18 | Add examples | `examples/` | ✅ |

---

## Configuration

### Config Struct

```go
type Config struct {
    ClientID             string   `json:"clientId,omitempty" yaml:"clientId,omitempty"`
    Hostname             string   `json:"hostname,omitempty" yaml:"hostname,omitempty"`
    DefaultScopes        []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty"`
    AppID                string   `json:"appId,omitempty" yaml:"appId,omitempty"`
    InstallationID       string   `json:"installationId,omitempty" yaml:"installationId,omitempty"`
    PrivateKey           string   `json:"privateKey,omitempty" yaml:"privateKey,omitempty"`
    PrivateKeyPath       string   `json:"privateKeyPath,omitempty" yaml:"privateKeyPath,omitempty"`
    PrivateKeySecretName string   `json:"privateKeySecretName,omitempty" yaml:"privateKeySecretName,omitempty"`
}
```

### Defaults

```go
func DefaultConfig() *Config {
    return &Config{
        ClientID:      "Ov23li6xn492GhPmt4YG",
        Hostname:      "github.com",
        DefaultScopes: []string{"gist", "read:org", "repo", "workflow"},
    }
}
```

### Global Config Addition

```go
type GlobalAuthConfig struct {
    Entra  *EntraAuthConfig  `json:"entra,omitempty" ...`
    GitHub *GitHubAuthConfig `json:"github,omitempty" ...`
}

type GitHubAuthConfig struct {
    ClientID             string   `json:"clientId,omitempty" ...`
    Hostname             string   `json:"hostname,omitempty" ...`
    DefaultScopes        []string `json:"defaultScopes,omitempty" ...`
    AppID                string   `json:"appId,omitempty" ...`
    InstallationID       string   `json:"installationId,omitempty" ...`
    PrivateKeyPath       string   `json:"privateKeyPath,omitempty" ...`
    PrivateKey           string   `json:"privateKey,omitempty" ...`
    PrivateKeySecretName string   `json:"privateKeySecretName,omitempty" ...`
}
```

---

## Secret Naming Convention

Following the established pattern from the Entra handler:

```
scafctl.auth.github.<type>
```

| Secret Name | Description |
|-------------|-------------|
| `scafctl.auth.github.refresh_token` | OAuth refresh token (if token expiration is enabled) |
| `scafctl.auth.github.metadata` | Token metadata (claims, hostname, client ID, expiry) |
| `scafctl.auth.github.token.<flow>.<fingerprint>.<scope-hash>` | Cached access tokens partitioned by flow, config identity, and scope |

---

## Environment Variables

### PAT Flow

| Variable | Description | Priority |
|----------|-------------|----------|
| `GITHUB_TOKEN` | GitHub personal access token or Actions token | 1 (highest) |
| `GH_TOKEN` | GitHub personal access token (gh CLI convention) | 2 |

### GitHub App Flow

| Variable | Description |
|----------|-------------|
| `SCAFCTL_GITHUB_APP_ID` | GitHub App ID (overrides config) |
| `SCAFCTL_GITHUB_APP_INSTALLATION_ID` | Installation ID (overrides config) |
| `SCAFCTL_GITHUB_APP_PRIVATE_KEY` | Inline PEM private key (overrides config) |
| `SCAFCTL_GITHUB_APP_PRIVATE_KEY_PATH` | Path to PEM private key file (overrides config) |

### GHES Configuration

| Variable | Description |
|----------|-------------|
| `GH_HOST` | GitHub hostname (alternative to `--hostname` flag) |

---

## Claims Mapping

GitHub `/user` API response mapped to `auth.Claims`:

| Claims Field | GitHub Field | Example |
|-------------|-------------|---------|
| `Subject` | `login` | `"octocat"` |
| `Name` | `name` | `"The Octocat"` |
| `Email` | `email` | `"octocat@github.com"` |
| `ObjectID` | `id` (as string) | `"1"` |
| `Username` | `login` | `"octocat"` |
| `Issuer` | `"github.com"` or GHES hostname | `"github.com"` |

---

## CLI UX

### Login

```bash
# Interactive login with browser OAuth + PKCE (default)
scafctl auth login github

# Headless / SSH fallback
scafctl auth login github --flow device-code

# Login to GitHub Enterprise Server
scafctl auth login github --hostname github.example.com

# Login with custom client ID
scafctl auth login github --client-id abc123

# Login with specific scopes
scafctl auth login github --scope repo --scope read:org

# Login with PAT (requires env var)
scafctl auth login github --flow pat

# Login with GitHub App installation token
scafctl auth login github --flow github-app

# Login with custom callback port (for fixed redirect URI)
scafctl auth login github --callback-port 8400
```

### Status

```bash
scafctl auth status
# Handler: github
# Status: Authenticated
# Identity: octocat
# Hostname: github.com
# Scopes: gist, read:org, repo, workflow
```

### Token

```bash
# Get access token for debugging
scafctl auth token github
```

### Logout

```bash
scafctl auth logout github
```

---

## Error Handling

| Error | Condition | User Message |
|-------|-----------|-------------|
| `ErrNotAuthenticated` | No stored credentials / no env var | `not authenticated: please run 'scafctl auth login github'` |
| `ErrTokenExpired` | Refresh token expired | `credentials expired: please run 'scafctl auth login github'` |
| `ErrAuthenticationFailed` | Invalid PAT or OAuth failure | `authentication failed: <details>` |
| `ErrTimeout` | Device code flow timed out | `authentication timed out` |
| `ErrUserCancelled` | User cancelled device code flow | `authentication cancelled by user` |

---

## Implementation Order (Completed)

All phases were implemented in order: **Phase 1 (core handler)** → **Phase 2 (additional flows: auth code + PKCE, GitHub App)** → **Phase 3 (CLI wiring)** → **Phase 4 (testing)** → **Phase 5 (docs & examples)**.

---

## References

- [GitHub OAuth Authorization Code Flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#web-application-flow)
- [GitHub OAuth Device Flow Docs](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow)
- [GitHub Token Expiration](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/refreshing-user-access-tokens)
- [GitHub App Authentication](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/about-authentication-with-a-github-app)
- [GitHub App Installation Access Tokens](https://docs.github.com/en/rest/apps/apps#create-an-installation-access-token-for-an-app)
- [GitHub REST API - Users](https://docs.github.com/en/rest/users/users#get-the-authenticated-user)
- [PKCE (RFC 7636)](https://tools.ietf.org/html/rfc7636)
- [Entra Handler (reference implementation)](../pkg/auth/entra/)

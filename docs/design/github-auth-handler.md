---
title: "GitHub Auth Handler"
weight: 6
---

# GitHub Auth Handler Implementation Plan

## Overview

Implement a builtin GitHub auth handler (`github`) following the established patterns from the Entra handler. The handler will support OAuth device code flow for interactive use and PAT (Personal Access Token) from environment variables for CI/CD.

---

## Design Decisions

### Authentication Flows

| Flow | Use Case | Mechanism |
|------|----------|-----------|
| **Device Code** | Interactive CLI use | OAuth 2.0 device authorization grant via GitHub OAuth App |
| **PAT** | CI/CD pipelines, automation | Read `GITHUB_TOKEN` or `GH_TOKEN` from environment variables |

**Rationale**: Device code flow is the standard for CLI tools (`gh`, `az`, `gcloud` all use it). PAT from environment mirrors the Entra handler's service principal pattern and aligns with GitHub Actions' `GITHUB_TOKEN` injection.

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
| Device code request | `POST https://github.com/login/device/code` |
| Token poll / exchange | `POST https://github.com/login/oauth/access_token` |
| Token refresh | `POST https://github.com/login/oauth/access_token` (with `grant_type=refresh_token`) |
| User info (claims) | `GET https://api.github.com/user` |

All endpoints accept and return JSON when `Accept: application/json` is set.

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

---

## File Structure

```
pkg/auth/github/
├── cache.go              # Token caching (reuse Entra pattern)
├── cache_test.go
├── claims.go             # GitHub user → auth.Claims mapping
├── claims_test.go
├── config.go             # Config struct, defaults, validation
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

### Phase 2: Wiring (Tasks 7-11) — ✅ Complete

| # | Task | Files | Status |
|---|------|-------|--------|
| 7 | Add `FlowPAT` flow constant | `pkg/auth/handler.go` | ✅ |
| 8 | Add GitHub config to global config | `pkg/config/types.go` | ✅ |
| 9 | Wire into root command | `pkg/cmd/scafctl/root.go` | ✅ |
| 10 | Wire into CLI auth commands | `pkg/cmd/scafctl/auth/handler.go` | ✅ |
| 11 | Update login command | `pkg/cmd/scafctl/auth/login.go` | ✅ |

### Phase 3: Testing (Tasks 12-13) — ✅ Complete

| # | Task | Files | Status |
|---|------|-------|--------|
| 12 | Write unit tests | `pkg/auth/github/*_test.go` | ✅ (31 tests) |
| 13 | Write CLI integration tests | `tests/integration/cli_test.go` | ✅ |

### Phase 4: Documentation (Tasks 14-16) — ✅ Complete

| # | Task | Files | Status |
|---|------|-------|--------|
| 14 | Update auth design doc | `docs/design/auth.md` | ✅ |
| 15 | Update auth tutorial | `docs/tutorials/auth-tutorial.md` | ✅ |
| 16 | Add examples | `examples/` | ✅ |

---

## Configuration

### Config Struct

```go
type Config struct {
    ClientID      string   `json:"clientId,omitempty" yaml:"clientId,omitempty"`
    Hostname      string   `json:"hostname,omitempty" yaml:"hostname,omitempty"`
    DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty"`
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
    ClientID      string   `json:"clientId,omitempty" ...`
    Hostname      string   `json:"hostname,omitempty" ...`
    DefaultScopes []string `json:"defaultScopes,omitempty" ...`
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
| `scafctl.auth.github.token.<scope-hash>` | Cached access tokens by scope |

---

## Environment Variables

### PAT Flow

| Variable | Description | Priority |
|----------|-------------|----------|
| `GITHUB_TOKEN` | GitHub personal access token or Actions token | 1 (highest) |
| `GH_TOKEN` | GitHub personal access token (gh CLI convention) | 2 |

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
# Interactive login with device code flow (default)
scafctl auth login github

# Login to GitHub Enterprise Server
scafctl auth login github --hostname github.example.com

# Login with custom client ID
scafctl auth login github --client-id abc123

# Login with specific scopes
scafctl auth login github --scope repo --scope read:org

# Login with PAT (requires env var)
scafctl auth login github --flow pat
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

All phases were implemented in order: **Phase 1 (core handler)** → **Phase 2 (CLI wiring)** → **Phase 3 (testing)** → **Phase 4 (docs & examples)**.

---

## References

- [GitHub OAuth Device Flow Docs](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow)
- [GitHub Token Expiration](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/refreshing-user-access-tokens)
- [GitHub REST API - Users](https://docs.github.com/en/rest/users/users#get-the-authenticated-user)
- [Entra Handler (reference implementation)](../pkg/auth/entra/)

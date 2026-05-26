---
title: "Multi-Profile Authentication"
weight: 23
---

# Multi-Profile Authentication

**Issue**: [#283](https://github.com/oakwood-commons/scafctl/issues/283)
**Status**: Design
**Supersedes**: [#270](https://github.com/oakwood-commons/scafctl/issues/270) (closed)

---

## Problem

Users can only store one set of credentials per auth handler. This blocks
workflows that require multiple identities for the same provider:

- **GitHub EMU**: An EMU account cannot contribute outside the EMU boundary,
  and external accounts cannot see inside. Users need both identities
  simultaneously.
- **Entra ID**: Different tenants, WIF vs user accounts, or separate service
  principals for different environments.
- **GCP**: Internal org account vs external personal account, each with
  different project and credential configurations.

Today, switching between identities requires `auth logout` + `auth login`,
which destroys the previous session. There is no way to target a specific
identity from solution YAML or in an MCP session.

---

## Prior Art

| Tool | Profile Model | Global Switch | Per-Invocation | Env Var |
|------|---------------|---------------|----------------|---------|
| GitHub CLI | Account per host | `gh auth switch` | N/A | `GH_TOKEN` |
| AWS CLI | Named profiles | `aws configure set` | `--profile name` | `AWS_PROFILE` |
| gcloud | Named configs | `gcloud config configurations activate` | `--configuration name` | `CLOUDSDK_ACTIVE_CONFIG_NAME` |
| Azure CLI | Subscription | `az account set` | `--subscription id` | `AZURE_SUBSCRIPTION_ID` |

**Common pattern**: flag > env var > config default > bare handler name.

---

## Design

### Profile Naming: `handler@profile`

Profiles use `handler@profile` syntax everywhere -- config, CLI, solution YAML:

~~~
github           # default profile (no @)
github@work      # named "work" profile
entra@prod       # named "prod" profile for Entra
gcp@internal     # named "internal" profile for GCP
~~~

Profile names must be non-empty, ASCII alphanumeric plus `-` and `_`, max 64
characters. The `@` character is reserved as the separator and cannot appear in
profile names.

### Single Instance, Dynamic Resolution

Each auth handler remains a **single instance** in the registry. Profile
resolution happens via `TokenOptions.Profile`:

~~~go
type TokenOptions struct {
    // ... existing fields ...
    Profile string // empty = default profile
}
~~~

**Why not N handler instances?**

| Aspect | N Instances | 1 Instance + Profile |
|--------|------------|----------------------|
| Remote/plugin handlers | N gRPC connections | 1 connection, profile in request |
| New profiles | Restart to re-register | Dynamic resolution |
| Registry complexity | Name collision handling | Unchanged |
| Handler interface | Unchanged | `TokenOptions` gains one field |

The single-instance approach is simpler and works uniformly for local, remote,
and plugin-hosted handlers.

### Secret Key Namespacing

Profile-specific credentials are namespaced in the secret store. The OAuth2
handler already uses a prefix-based key pattern (`scafctl.auth.oauth2.`):

~~~
scafctl.auth.oauth2.<handler>.refresh_token                # default profile
scafctl.auth.oauth2.<handler>.<profile>.refresh_token      # named profile
scafctl.auth.oauth2.<handler>.<profile>.metadata           # named profile
scafctl.auth.oauth2.<handler>.token.<scope-hash>           # default cache
scafctl.auth.oauth2.<handler>.<profile>.token.<scope-hash> # profile cache
~~~

Existing default-profile keys are unchanged -- zero migration for current users.

### Config Structure

Each handler config gains an optional `profiles` map. Profile configs inherit
from the handler's top-level config and override specific fields:

~~~yaml
auth:
  github:
    clientId: "default-client-id"
    defaultScopes: ["repo", "read:org"]
    activeProfile: "work"           # optional default profile
    profiles:
      work:
        clientId: "enterprise-client-id"
        hostname: "github.example.com"
        defaultScopes: ["repo", "read:org", "admin:org"]
      personal:
        hostname: "github.com"
        defaultScopes: ["repo"]

  entra:
    clientId: "default-entra-client"
    tenantId: "common"
    profiles:
      prod:
        tenantId: "prod-tenant-id"
        defaultScopes: ["api://prod/.default"]
      dev:
        tenantId: "dev-tenant-id"
        defaultScopes: ["api://dev/.default"]

  gcp:
    profiles:
      internal:
        project: "internal-project"
        impersonateServiceAccount: "sa@internal.iam.gserviceaccount.com"
      external:
        project: "external-project"
~~~

**Inheritance rule**: profile config is merged on top of the handler's top-level
config. Only fields explicitly set in the profile override the parent. This
avoids duplicating `clientId` across every profile when they share the same
OAuth app.

### Config Types

~~~go
// GitHubAuthConfig gains:
type GitHubAuthConfig struct {
    // ... existing fields ...
    ActiveProfile string                       `json:"activeProfile,omitempty" yaml:"activeProfile,omitempty"`
    Profiles      map[string]*GitHubAuthConfig `json:"profiles,omitempty" yaml:"profiles,omitempty"`
}

// Same pattern for EntraAuthConfig, GCPAuthConfig, CustomOAuth2Config.
// Profile structs reuse the same type with all fields optional (pointer or
// omitempty). Nested Profiles maps within a profile are ignored (no recursion).
~~~

### Solution YAML: `authProvider` Field

The existing `authProvider` field on provider inputs gains `handler@profile`
support:

~~~yaml
resolvers:
  emu-repos:
    resolve:
      with:
        - provider: github
          inputs:
            authProvider: github@work
            operation: list_repos

  oss-repos:
    resolve:
      with:
        - provider: github
          inputs:
            authProvider: github@personal
            operation: list_repos

  cloud-api:
    resolve:
      with:
        - provider: http
          inputs:
            authProvider: entra@prod
            url: https://api.example.com
            scope: "api://resource/.default"
~~~

Bare `authProvider: github` continues to use the default profile.

### Precedence

Resolution order (highest wins):

1. **Solution YAML** -- `authProvider: handler@profile` in provider inputs
2. **CLI flag** -- `--auth-profile <profile>` on the root command
3. **Environment variable** -- `SCAFCTL_AUTH_PROFILE=<profile>`
4. **Config default** -- `activeProfile` in handler config
5. **Bare handler** -- no profile, uses default credentials

### CLI Commands

Auth commands gain a `--profile` flag:

~~~bash
# Login with a named profile
scafctl auth login github --profile work

# Logout a specific profile
scafctl auth logout github --profile work

# Get token for a profile
scafctl auth token github --profile work

# Show status of all profiles across all handlers
scafctl auth status

# Switch default profile for a handler
scafctl auth switch github personal
~~~

The `auth status` output shows all profiles:

~~~
Handler     Profile     Status                  Identity
-------     -------     ------                  --------
entra       (built-in)  Authenticated           user@example.com
entra       work        Authenticated           user@work.example.com  [active]
github      (built-in)  Authenticated           abaker9
github      work        Authenticated           abaker9-emu
github      personal    Not authenticated
gcp         (built-in)  Authenticated           user@gmail.com
gcp         internal    Authenticated           user@corp.google.com   [active]
~~~

### Global Override

A persistent `--auth-profile` flag on the root command overrides the default
profile for all handlers in a single invocation:

~~~bash
# All auth in this run uses the "work" profile
scafctl run solution -f ./sol.yaml --auth-profile work

# Environment variable equivalent
SCAFCTL_AUTH_PROFILE=work scafctl run solution -f ./sol.yaml
~~~

This is a convenience for "run everything as my work identity" without
modifying solution YAML. Per-provider `authProvider: handler@profile` in the
solution still takes precedence.

### Plugin Auth Handler Forwarding

Plugin auth handlers remain profile-unaware. The host resolves profiles before
forwarding:

1. Provider input specifies `authProvider: github@work`
2. Host parses `handler@profile` via `ParseProfileKey()`
3. Host calls `handler.GetToken(ctx, TokenOptions{Profile: "work", ...})`
4. Host injects the resolved token into the plugin process via the existing
   auth injection protocol
5. Plugin receives a token -- never sees profile names

No plugin SDK changes are needed. Existing plugins work without modification.

### authProvider Parsing

A new `ParseProfileKey` helper handles the `handler@profile` syntax:

~~~go
// ParseProfileKey splits "handler@profile" into (handler, profile).
// Returns (key, "") for bare handler names.
func ParseProfileKey(key string) (handler, profile string) {
    if idx := strings.Index(key, "@"); idx >= 0 {
        return key[:idx], key[idx+1:]
    }
    return key, ""
}

// ProfileKey joins handler and profile into "handler@profile".
// Returns bare handler name when profile is empty.
func ProfileKey(handler, profile string) string {
    if profile == "" {
        return handler
    }
    return handler + "@" + profile
}
~~~

The HTTP provider's token injection path (`pkg/provider/builtin/httpprovider`)
uses `ParseProfileKey` to extract the profile before calling
`auth.GetHandler()` and passing the profile through `TokenOptions`.

---

## Affected Packages

| Package | Change |
|---------|--------|
| `pkg/auth/handler.go` | `TokenOptions` gains `Profile` field (SDK type -- coordinate with SDK) |
| `pkg/auth/profile.go` | New file: `ParseProfileKey`, `ProfileKey`, `ValidateProfileName` |
| `pkg/auth/oauth2/handler.go` | Profile-aware secret key construction, config merging |
| `pkg/auth/official/` | No changes (handlers are profile-unaware at the official registry level) |
| `pkg/config/types.go` | `ActiveProfile` + `Profiles` map on auth config structs |
| `pkg/cmd/scafctl/auth/login.go` | `--profile` flag |
| `pkg/cmd/scafctl/auth/logout.go` | `--profile` flag |
| `pkg/cmd/scafctl/auth/token.go` | `--profile` flag |
| `pkg/cmd/scafctl/auth/status.go` | Multi-profile display |
| `pkg/cmd/scafctl/auth/switch.go` | New subcommand |
| `pkg/cmd/scafctl/root.go` | `--auth-profile` persistent flag, `SCAFCTL_AUTH_PROFILE` env var |
| `pkg/provider/builtin/httpprovider/` | `ParseProfileKey` in authProvider parsing |
| `pkg/provider/builtin/identityprovider/` | Profile-aware identity resolution |
| `pkg/plugin/wrapper_auth.go` | Pass profile through to `GetToken` |
| `pkg/mcp/tools_auth.go` | Profile parameter in auth status tool |
| `pkg/settings/` | `AuthProfile` field on `Run` for embedder access |

---

## Implementation Phases

### Phase 1: Core Infrastructure + GitHub

**Goal**: A user can `auth login github --profile work` and use
`authProvider: github@work` in solution YAML.

1. Add `Profile string` to the auth SDK `TokenOptions` type
2. Create `pkg/auth/profile.go`: `ParseProfileKey`, `ProfileKey`,
   `ValidateProfileName`
3. OAuth2 handler: profile-aware secret key construction
   (`secretKeyWithProfile`)
4. OAuth2 handler: profile-aware `Login`, `Logout`, `GetToken`, `Status`
5. Config types: `ActiveProfile` + `Profiles` map on `GitHubAuthConfig`
6. OAuth2 handler: config merging (profile inherits from parent, overrides
   specific fields)
7. `--profile` flag on `auth login`, `auth logout`, `auth token`
8. `auth status`: show all profiles per handler
9. HTTP provider: parse `handler@profile` from `authProvider` input, pass
   profile to `TokenOptions`
10. Tests: unit tests for all new code, integration test for login/token/status
    with profile

### Phase 2: Global Override + Entra/GCP + Switch

**Goal**: `--auth-profile` flag works globally. Entra and GCP handlers support
profiles.

1. `--auth-profile` persistent flag on root command
2. `SCAFCTL_AUTH_PROFILE` env var binding
3. Precedence resolution: solution > flag > env > config > default
4. `scafctl auth switch` interactive command
5. `ActiveProfile` + `Profiles` on `EntraAuthConfig` and `GCPAuthConfig`
6. Tests: precedence integration tests, Entra/GCP profile tests

### Phase 3: Plugin Forwarding + MCP + Polish

**Goal**: Plugin-hosted handlers and MCP surface support profiles.

1. Plugin host: parse profile from `authProvider`, pass to `GetToken`
2. Identity provider: profile-aware claims/status
3. MCP `auth_status` tool: optional `profile` parameter
4. Config validation: `ValidateProfileName` in config load path
5. Custom OAuth2 handlers: profile support
6. Documentation updates

---

## Risks and Mitigations

### Secret Key Stability

Default profile keys are unchanged. Existing users never see a behavior change.
Named profiles use new namespaced keys that don't collide with existing ones.

### Backward Compatibility

- Empty `Profile` = default profile = existing behavior
- Missing `profiles` map = existing behavior
- Bare `authProvider: github` = existing behavior
- No handler interface changes (only `TokenOptions` struct gains a field)

### SDK Coordination

`TokenOptions` is aliased from the auth SDK (`sdkauth.TokenOptions`). The
`Profile` field must be added in the SDK first, then the alias picks it up.
If the SDK is slow to update, a local `ProfileTokenOptions` wrapper can bridge
the gap temporarily.

### Config Inheritance Edge Cases

Profile config merging must handle:
- Nil parent fields (profile provides the value)
- Zero-value vs unset (use pointer types in profile config to distinguish)
- `HTTPClient` overrides (profile can override timeout but inherit base URL)

Recommendation: use a dedicated merge function per config type rather than
generic reflection-based merging. This keeps behavior explicit and testable.

### Solution Portability

Solutions referencing `authProvider: github@work` assume the `work` profile
exists on the runner's machine. Document that:
- Named profiles are optional -- solutions should work with bare handler names
  when profile-specific behavior is not required
- CI/CD environments should configure profiles via config files or env vars

### Plugin Handlers

Plugin handlers are profile-unaware by design. The host resolves the profile
and injects the correct token. If a plugin handler needs profile-specific
**config** (e.g., different hostname), the host must merge the profile config
before passing settings via `injectAuthHandlerSettings`.

### Shared OAuth Sessions (Future Optimization)

AWS CLI lets multiple profiles share one `sso-session`, so you login once and
get tokens for multiple accounts/roles. The same pattern applies here: if two
profiles share the same OAuth app (`clientId`), they could share a refresh
token and differ only by the account that authorized it. This avoids redundant
login flows when profiles target different accounts on the same identity
provider.

This is a Phase 2+ optimization -- not needed for the initial implementation.

### Directory-Based Auto-Switching

The `SCAFCTL_AUTH_PROFILE` env var enables automatic profile switching using
tools like [direnv](https://direnv.net/). Users can place an `.envrc` file in
their repo root to auto-activate the correct profile:

~~~bash
# .envrc in EMU/work repos
export SCAFCTL_AUTH_PROFILE=work
~~~

~~~bash
# .envrc in personal/OSS repos
export SCAFCTL_AUTH_PROFILE=personal
~~~

This requires no code changes -- the env var support in Phase 2 enables it
automatically. Document this pattern in the auth tutorial and README.

gcloud uses `CLOUDSDK_ACTIVE_CONFIG_NAME` with the same direnv approach and
it's one of their most popular workflow patterns.

---

## Open Questions

1. **Profile-scoped config for plugin handlers**: Should `injectAuthHandlerSettings`
   merge profile config before sending to the plugin? Or should the plugin receive
   the full profiles map and resolve internally?

2. **Profile auto-discovery**: Should `auth login github` (without `--profile`)
   detect if the user already has a default profile and prompt to name the new
   one? Or is explicit `--profile` always required for non-default profiles?

3. **Profile deletion**: Should there be a `scafctl auth profile delete` command,
   or is `auth logout github --profile work` sufficient (clears creds but leaves
   config)?

4. **Custom OAuth2 handlers**: The `CustomOAuth2Config` struct has a `Name` field
   that serves as the handler name. Should profiles be supported per custom
   handler, or only for official handlers initially?

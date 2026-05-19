# feat(api): Provider security, plugin lifecycle, and auth identity separation for API mode

## Summary

When scafctl runs as an API server (`scafctl serve`), the provider/plugin
subsystem and authentication layer must handle concerns absent from CLI mode:
restricting dangerous providers, controlling plugin version sprawl, separating
caller identity from server identity, and supporting delegated auth flows (OBO,
token pass-through). This design must achieve feature parity with the legacy
version of this tool's API server.

This issue covers three interconnected areas:

1. **Provider security and plugin lifecycle** -- which providers are allowed,
   how versions are managed, how plugin processes are kept healthy
2. **Auth identity separation** -- solutions run as the caller, never as the
   server
3. **Legacy feature parity** -- OBO, machine/user detection, GitHub PAT
   pass-through, downstream URL restrictions

## Problem

### Provider/Plugin Gaps

- Providers like `exec` and `goscript` (future) allow arbitrary code execution
  -- dangerous in a multi-tenant API server
- No mechanism to prevent unbounded provider version sprawl (one pod could
  accumulate dozens of plugin binaries)
- Plugin processes are idle-evicted after 5 minutes, causing cold-start latency
  on subsequent requests for pre-loaded official providers
- No deny list for provider names -- only allow lists

### Auth Identity Gaps

- `scafctl serve` validates inbound bearer tokens via OIDC but discards the raw
  token after validation
- Solutions running in API mode use the server's own credentials, not the
  caller's
- No On-Behalf-Of (OBO) token exchange for Entra-protected downstream APIs
- No mechanism for callers to supply supplemental tokens (e.g., GitHub PAT) for
  non-OBO providers
- No machine vs user identity detection (`idtyp` claim) for grant type
  selection
- No per-request OBO token cache with singleflight deduplication
- No downstream URL restriction for delegated tokens

### Legacy Features Not Yet in scafctl

| Feature | Legacy API Server | scafctl |
|---------|--------------|---------|
| OIDC validation | Yes | Yes |
| Raw token preserved in context | Yes | No |
| `idtyp` claim parsing (machine vs user) | Yes | No |
| Machine caller -> `client_credentials` | Yes | No |
| User caller -> OBO grant | Yes | No |
| Per-request OBO cache + singleflight | Yes | No |
| GitHub PAT via header/body | Yes | No |
| GitHub PAT regex validation | Yes | No |
| Allowed downstream URL regex | Yes | No |
| `__request` context for CEL/templates | N/A (no CEL) | No (#376) |

## Landscape: How Comparable Tools Handle This

| Tool | Plugin Model | Server-Mode Strategy | Versioning |
|------|-------------|---------------------|------------|
| **Terraform Enterprise** | gRPC processes (go-plugin) | Filesystem/network mirror: admins pre-stage approved providers. No on-demand download | Lock file pins exact version + checksums |
| **Caddy** | Compile-time Go imports | Single binary with all modules baked in. `xcaddy build` creates custom binary | Single binary = single version of everything |
| **Backstage** | In-process TypeScript (DI container) | Plugins installed at build time via `yarn add`. No runtime loading | Package manager lock files |

Key takeaways:

- No production server tool allows unbounded on-demand plugin download
- Version sets are fixed per deployment
- Dangerous capabilities are architecturally excluded

## Design

### Part 1: Provider Security Tiers

Introduce a provider classification that determines availability by runtime
context:

| Tier | Providers | CLI | API | MCP |
|------|-----------|-----|-----|-----|
| **Safe** | static, cel, http, file, validation, debug, gotmpl, message, parameter, directory, env, git, hcl, identity, metadata, secret, sleep | Yes | Yes | Yes |
| **Privileged** | exec, goscript (future) | Yes | **Deny by default** | **Deny by default** |
| **Custom** | Any third-party plugin | Yes | **Explicit allowlist** | **Explicit allowlist** |

This extends the existing `APIPluginConfig.AllowExternal` + `AllowedPlugins`.
The new piece is a **built-in deny list** for specific provider names in API/MCP
mode.

Add `DeniedPlugins` to `APIPluginConfig` (defaulting to `["exec"]`). Enforce in
the plugin pool's `ensureOne()` and in `provider.Run()` for built-in providers.
Defense-in-depth: even if someone misconfigures the allowlist, `exec` won't run
unless explicitly un-denied.

### Part 2: Version Pinning Strategy

Modeled after Terraform Enterprise's filesystem mirror approach:

1. **API server declares a provider manifest** -- a list of
   `(provider, version)` tuples loaded at startup. Only these versions are
   available
2. **Solution authors pin to major versions** -- semver constraints (e.g.,
   `github: "^1"`) resolved against the server's manifest
3. **No on-demand download in API mode** -- all providers fetched at startup (or
   via init container). Existing `preloadOfficialProviders()` already does this
   for official providers
4. **CLI retains on-demand behavior** -- no change

### Part 3: Plugin Pool Optimizations

The current gRPC plugin pool is architecturally sound. Per-call overhead is
~0.1ms on localhost. Optimizations:

| Optimization | Impact |
|-------------|--------|
| Disable idle eviction for pre-loaded providers | Eliminates cold-start surprises |
| Verify gRPC keepalive settings for long-lived connections | Connection stability |
| Retry-after-health-check on execution failure | Self-healing |
| Pool metrics (active plugins, evictions, spawn latency) | Observability |

gRPC remains the right transport. Alternatives considered and rejected:

- **In-process/WASM**: breaks plugin SDK contract, loses process isolation
- **Unix sockets**: marginal improvement (~10-20%), not architecturally
  significant
- **HTTP/REST**: higher overhead, no streaming, worse in every dimension

### Part 4: Auth Identity Separation

Two distinct identity contexts that must never be conflated:

| Identity | Purpose | Examples | Source |
|----------|---------|----------|--------|
| **Caller** | Solution-level work on behalf of the user | Querying Graph API, creating GitHub PRs | Inbound JWT, supplemental tokens |
| **Server** | Infrastructure/operational concerns | Reading solutions from storage, telemetry, catalog access | WIF, service principal, managed identity |

**Core invariant: solutions always run as the caller, never as the server.**

#### Auth Scope Model

~~~go
type Scope string

const (
    ScopeCaller Scope = "caller"
    ScopeServer Scope = "server"
)
~~~

API endpoints set `ScopeCaller` on the solution execution context. Auth handlers
dispatch based on scope:

| Handler | ScopeCaller (user) | ScopeCaller (machine) | ScopeServer |
|---------|-------------------|----------------------|-------------|
| **Entra** | OBO grant | client_credentials | WIF / SP |
| **GitHub** | Pass-through (caller's PAT) | Pass-through | GitHub App installation token |
| **GCP** | STS token exchange or pass-through | STS exchange | ADC / WIF / metadata |
| **Custom OAuth2** | Pass-through only | Pass-through | Client credentials |

### Part 5: Machine vs User Detection

The inbound token's `idtyp` claim determines the grant type:

| Caller | `idtyp` | Grant | Downstream sees |
|--------|---------|-------|-----------------|
| User (browser, CLI) | absent | OBO (`jwt-bearer`) | User's identity |
| Service principal | `"app"` | `client_credentials` | API server's identity |

A machine caller has no user to act on behalf of -- OBO would fail at Azure.
The API must detect this and use `client_credentials` instead.

### Part 6: OBO Token Cache

Within a single solution execution, multiple resolvers may need the same
downstream token. For example, two resolvers calling Graph API with scope
`https://graph.microsoft.com/.default` should reuse the same OBO token, not
trigger two separate token exchanges. At the same time, tokens from one
caller must never leak to another caller's request.

**Two-tier cache design:**

**Tier 1 -- Per-request cache (required):**
- Initialized per HTTP request via middleware
- Key: `sha256(assertion)[0:16] | scope`
- 30-second expiry buffer before token `exp`
- `singleflight` deduplication for concurrent resolver executions within
  the same request (DAG parallelism means multiple resolvers may request
  the same scope simultaneously)
- Destroyed when the request completes -- no cross-request leakage

**Tier 2 -- Global LRU cache (optional, for high-throughput):**
- Opt-in via `delegation.oboCacheMode: "global"`
- Key: `sha256(full_assertion) | scope` (full hash, not truncated)
- TTL: token lifetime minus 2-minute buffer
- Max entries: configurable (default 10000)
- Safe because: same assertion = same caller token = same user. A token
  can only be reused if the caller presents the exact same JWT, which only
  the original caller possesses
- Benefits: the same user making 100 API calls in 10 seconds triggers 1
  OBO exchange instead of 100. Prevents hitting Azure token endpoint rate
  limits under load

Lookup order: per-request cache -> global cache -> Azure token endpoint.

### Part 7: Supplemental Token Pass-Through (GitHub PAT)

GitHub has no OBO equivalent. Callers provide their own token via:

1. `X-Authorization-Github-Cloud` header (preferred)
2. `X-Github-Token` header (backward compat)
3. `githubToken` field in request body

PAT format validated with regex:

~~~
^(?:gh[pso]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9]+_[A-Za-z0-9_]+|[A-Fa-f0-9]{40,72})$
~~~

When `AuthScope == ScopeCaller` and handler is `github`: check supplemental
tokens, use if found, fail if not (never fall back to server's GitHub App
credentials for solution work).

### Part 8: Downstream URL Restriction

`allowedDownstreamURLs` regex patterns control where delegated tokens can be
sent. Enforced in the HTTP provider when `AuthScope == ScopeCaller`.
Defense-in-depth on top of `allowedOBOScopes`.

### Part 9: `__request` Context Variable (#376)

Expose request context to CEL/templates in API mode:

- `__request.claims` -- parsed JWT claims (always available)
- `__request.callerType` -- `"user"` or `"app"`
- `__request.headers` -- filtered (no Authorization, Cookie by default)
- `__request.token` -- raw bearer token (**opt-in only**, requires
  `exposeCallerToken: true`)
- `__request.tokens.github` -- supplemental tokens (opt-in)

## Configuration Shape

~~~yaml
apiServer:
  host: "0.0.0.0"
  port: 8080

  # Inbound auth -- validates callers (existing)
  auth:
    tenantId: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
    clientId: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"

  # Server's own identity -- for infra operations only
  identity:
    entra:
      tenantId: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      clientId: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
      clientSecret:
        secretRef: "SCAFCTL_API_ENTRA_CLIENT_SECRET"
    github:
      appId: 12345
      installationId: 67890
      privateKey:
        secretRef: "SCAFCTL_API_GITHUB_PRIVATE_KEY"
    gcp:
      project: "my-project"

  # Caller identity delegation
  delegation:
    exposeCallerToken: false
    requireCallerAuth: true
    allowedOBOScopes:
      - "https://graph.microsoft.com/.default"
      - "api://downstream-api/.default"
    allowedDownstreamURLs:
      - "https://graph\\.microsoft\\.com/.*"
      - "https://management\\.azure\\.com/.*"

  # Plugin restrictions
  plugins:
    allowExternal: false
    deniedPlugins: [exec]
    providerVersions:
      github: "1.2.0"
      directory: "1.0.0"
    allowOnDemandFetch: false
    allowedCatalogs: [internal]
~~~

## Middleware Stack Order

~~~
1.  Recoverer
2.  RequestID
3.  StripSlashes
4.  RequestLogging
-- versioned routes (/v1/*) --
5.  CORS
6.  RequestTimeout
7.  Throttle
8.  OBOCacheMiddleware            (NEW)
9.  AuthMiddleware (OIDC)         (UPDATED: preserves raw token, parses idtyp)
10. SupplementalTokenMiddleware   (NEW: extracts GitHub PAT etc.)
11. RateLimit
12. MaxBodySize
13. Compression
14. SecurityHeaders
15. Metrics
16. AuditLogging
17. Tracing
~~~

## Security Invariants

1. **Solutions NEVER get server credentials.** `ScopeCaller` + no caller token =
   fail, never fallback to server identity
2. **Machine callers get `client_credentials`, not OBO.** Detect via
   `idtyp=app`
3. **`__request.token` is opt-in.** Disabled by default. Solutions see
   `__request.claims` without opt-in
4. **OBO scopes are restricted.** `allowedOBOScopes` prevents arbitrary
   downstream token minting
5. **Downstream URLs are restricted.** HTTP provider won't send delegated tokens
   to unallowed URLs
6. **GitHub PATs are validated.** Regex ensures only well-formed tokens are
   accepted
7. **Supplemental tokens are per-handler.** A GitHub PAT can't be used for
   Entra-scoped operations
8. **OBO cache is per-request.** No cross-user token leakage
9. **Server identity is invisible to CEL/templates.** Not in `__request`,
   `__execution`, or resolver output
10. **Privileged providers denied by default in API mode.** `exec` blocked
    unless explicitly un-denied

## Implementation Phases

Phases are grouped by dependency chain. Each phase is independently
shippable and testable.

~~~
Phase A (sandboxing)          Phase E (plugin lifecycle)
    │                              (independent)
    ▼
Phase B (auth middleware + __request)
    │
    ├──────────────────┐
    ▼                  ▼
Phase C (OBO/delegation)   Phase D (supplemental tokens)
~~~

A must ship before B. B must ship before C and D. E is fully independent.
C and D are independent of each other.

### Phase A: Provider Sandboxing (ship first, no dependencies)

Make `scafctl serve` safe to run before adding any auth delegation. Without
this, everything else is security theater -- a solution can read the
server's own credentials via `file` or `env`.

| Work Item | Size | Key Files |
|-----------|------|-----------|
| Add "Restricted" provider tier + `apiMode` context flag | S | `pkg/provider/`, `pkg/settings/` |
| `env` provider: allowed prefix list in API mode (deny by default) | S | plugin provider |
| `file`/`directory`: path containment to sandbox dir, reject absolute paths | M | plugin providers |
| `parameter`: disable `file://` protocol in API mode | S | builtin |
| `secret`: deny in API mode (or namespace restrict) | S | plugin |
| `identity`: return caller claims when in API mode | S | plugin |
| `sleep`: enforce max duration = request timeout | S | plugin |
| Hard-override `allowEnvFunctions: false` in API server startup | S | `pkg/api/` |
| Provider deny list (`DeniedPlugins` config, default `["exec"]`) | S | `pkg/config/`, `pkg/plugin/pool.go` |

### Phase B: Auth Middleware + `__request` Context (depends on A)

Preserve caller identity in context so solutions can see who is calling,
without any token exchange. Delivers value on its own -- solutions can
branch on caller identity without OBO.

| Work Item | Size | Key Files |
|-----------|------|-----------|
| Preserve raw bearer token in context after OIDC validation | S | `pkg/api/middleware/auth.go` |
| Parse `idtyp` claim, add `CallerType()` method | S | `pkg/api/middleware/auth.go` |
| Define `__request` CEL/template variable (claims, callerType, filtered headers) | M | `pkg/celexp/`, `pkg/gotmpl/`, resolves #376 |
| Opt-in `__request.token` behind `exposeCallerToken` config | S | `pkg/config/` |
| PII filtering on `__request.claims` (non-PII default, opt-in full) | S | new helper |

### Phase C: OBO + Downstream Restrictions (depends on B)

Enable caller-identity delegation to downstream APIs. Downstream URL
restriction and scope allowlisting ship in the same PR as OBO enablement --
no security window.

| Work Item | Size | Key Files |
|-----------|------|-----------|
| Per-request OBO cache with singleflight | M | new `pkg/auth/obocache/` |
| Optional global LRU cache tier (opt-in) | M | same package |
| `OBOCacheMiddleware` in API middleware stack | S | `pkg/api/` |
| `idtyp`-based grant dispatch (user -> OBO, machine -> client_credentials) | M | `pkg/auth/entra/` |
| `allowedOBOScopes` enforcement | S | `pkg/config/`, entra handler |
| Downstream URL restriction (parsed host+scheme allowlist, not regex) | M | `pkg/config/`, HTTP provider |
| `apiServer.delegation` config block (safe defaults = disabled) | S | `pkg/config/types.go` |
| `apiServer.identity.entra` config (client ID/secret/cert for OBO) | S | `pkg/config/types.go` |
| Audit log events for every token exchange | S | structured logging |

### Phase D: Supplemental Tokens (depends on B, independent of C)

Let callers supply non-OBO tokens for providers that need them (GitHub,
etc.). GitHub has no OBO, so this is pure pass-through.

| Work Item | Size | Key Files |
|-----------|------|-----------|
| `SupplementalTokens` type + context helpers | S | new in `pkg/auth/` |
| GitHub PAT regex validation | S | same |
| Extraction middleware (header + body, body deprecated from day 1) | S | `pkg/api/middleware/` |
| Wire GitHub auth handler `ScopeCaller` to supplemental lookup | S | `pkg/auth/` |
| `__request.tokens.github` (opt-in) | S | CEL/template context |
| Redact `githubToken` in audit/error paths | S | middleware |

### Phase E: Plugin Lifecycle Hardening (independent, ship anytime)

Version pinning, eviction control, pool metrics. Operational stability, not
security. No dependency on A-D.

| Work Item | Size | Key Files |
|-----------|------|-----------|
| `ProviderVersions map[string]string` config + pinned preload | M | `pkg/config/`, `pkg/plugin/pool.go` |
| `AllowOnDemandFetch` flag (default false in API mode) | S | `pkg/config/`, catalog fetch path |
| `WithDisableEvictionForAdopted` pool option | S | `pkg/plugin/pool.go` |
| gRPC keepalive settings | S | pool.go |
| Retry-after-health-check in `ProviderWrapper.Execute()` | S | pool.go |
| Pool metrics (active processes, evictions, health check failures) | S | pool.go |
| Lint rule: warn when solutions pin exact versions instead of major ranges | S | `pkg/lint/` |

## Risks

| Risk | Mitigation |
|------|-----------|
| OBO requires confidential client registration | Validate at startup: Entra auth enabled + delegation configured but no client secret/cert = fatal error with clear message |
| Token leakage in resolver output | `__request.token` opt-in + redaction in audit logs + lint warning when `__request.token` appears in output fields |
| Fallback to server identity | Hard fail, never fallback. This is the core invariant |
| GitHub has no OBO | Accept limitation. Document that GitHub-scoped API solutions require callers to provide their own token |
| Legacy clients send `githubToken` in body | Support body field for backward compat, prefer header. Deprecation warning in logs |
| Downstream URL regex misconfiguration | Compile at startup, fail fast on invalid patterns |
| Token lifetime > solution duration | Per-request cache works for typical runs. ForceRefresh retry on 401 already exists in HTTP provider |
| GCP has no direct OBO equivalent | STS token exchange for Entra-to-GCP federation. Document as separate pattern. Not day-1 if legacy doesn't support it |
| Breaking change: solutions using `exec` in API mode | Acceptable per project rules. Document in CHANGELOG |
| Version pinning too restrictive | `allowOnDemandFetch: true` escape hatch for dev/test |

## Open Questions and Known Gaps

This section captures issues identified during design review that need
resolution before or during implementation.

### 1. The "Safe" Tier Classification Is Incomplete

Several providers classified as Safe have serious API-mode risks:

| Provider | Risk | Severity |
|----------|------|----------|
| **`env`** | Can read ANY named env var including `AZURE_CLIENT_SECRET`, k8s service account tokens. `get` operation has no prefix restriction | Critical |
| **`file`** | Accepts absolute paths with no restriction. Can read `/etc/passwd`, `/var/run/secrets/kubernetes.io/serviceaccount/token`, or any file the server process can access | Critical |
| **`directory`** | Can enumerate arbitrary directories on the server filesystem | High |
| **`identity`** | Currently returns the SERVER's auth handler identity (WIF/SP), not the API caller's. Leaks server identity claims in API mode | High |
| **`secret`** | Reads from the OS credential store. The server's keychain may contain secrets not intended for solution callers | High |
| **`parameter`** | Supports `file://` protocol for reading local files. Unrestricted in API mode | Medium |
| **`sleep`** | Can hold connections indefinitely. DoS vector when combined with concurrent request limits | Low |

**Action needed**: Add a "Restricted" tier for providers that need API-mode
sandboxing. Define per-provider restrictions:

- `env`: require an allowed prefix list in API mode (e.g., only
  `SOLUTION_*` vars). Deny by default -- solutions should not read
  container environment variables
- `file` / `directory`: enforce path containment to a designated sandbox
  directory, reject absolute paths. In API mode, solutions should have no
  access to the container filesystem beyond an explicit working directory
- `identity`: return caller's claims (from `__request.claims`), not
  server's, when `AuthScope == ScopeCaller`
- `secret`: deny entirely in API mode or restrict to caller-scoped secret
  namespaces
- `parameter`: disable `file://` protocol in API mode
- `sleep`: enforce a maximum duration matching the request timeout

**This extends beyond providers.** CEL and Go templates also have access to
container state that must be restricted in API mode:

- **Go templates**: `allowEnvFunctions: false` (the default) blocks sprig's
  `env` and `expandenv` functions. This is safe today but must remain
  enforced -- the API server should **hard-override** this to `false`
  regardless of config file, so an operator cannot accidentally enable it
- **CEL**: CEL expressions do not have direct env/file access today, but
  custom functions or future extensions could introduce it. The CEL
  environment for API-mode execution should be audited for any functions
  that access host state
- **Provider outputs in context**: If the `env` or `file` provider runs
  before being restricted, their outputs are available to downstream
  resolvers via `_.<resolverName>`. Restrictions must be enforced at the
  provider execution boundary, not just at the CEL/template level

**This is the single biggest gap in the current design.** Without provider
and template sandboxing, auth identity separation is undermined -- a
solution can read `/var/run/secrets/kubernetes.io/serviceaccount/token` via
the `file` provider or enumerate secrets via `env` and obtain the same
credentials the server uses.

A new **Phase 0: Provider and Template Sandboxing** should be added and
prioritized ahead of the deny list work.

### 2. Downstream URL Regex Is Bypassable

The `allowedDownstreamURLs` regex approach has issues:

- `https://graph\\.microsoft\\.com/.*` matches
  `https://graph.microsoft.com.evil.com/callback` (no anchors)
- Regex doesn't parse URLs -- port tricks, userinfo, percent-encoding can
  bypass naive patterns
- Host-based allowlisting with URL parsing is safer than regex

**Recommendation**: Use parsed URL host+scheme allowlisting instead of regex.
If regex is retained, require `^` and `$` anchors and validate at startup
that patterns aren't trivially bypassable.

### 3. Machine Identity Loses Caller Traceability

When ServiceA (a service principal) calls the API, `client_credentials`
mints a token for the **API server's identity**, not ServiceA's. Downstream
APIs see scafctl, not ServiceA. This means:

- Audit trails downstream lose the original caller
- Two different service principals calling the API produce
  indistinguishable downstream calls
- Downstream APIs can't do fine-grained authorization by calling service

The legacy version has this same limitation. **Document as known limitation.**
Consider logging the original SP's `oid` and passing it as a custom header
(e.g., `X-Original-Caller-OID`) for audit purposes.

### 4. Token Reuse Within and Across Requests

Two scenarios must both work correctly:

**Within a request**: Two resolvers in the same solution both call Graph API
with the same scope. Without caching, this triggers two OBO exchanges for
identical tokens. The per-request cache (Part 6 of the design) solves this
-- same assertion + same scope = cache hit.

**Across requests**: The same user hits the API 100 times in 10 seconds.
Per-request caching triggers 100 OBO exchanges. At scale, this will hit
Azure token endpoint rate limits.

The two-tier cache in Part 6 addresses both. The global tier is opt-in:

~~~yaml
apiServer:
  delegation:
    oboCacheMode: "per-request"  # or "global" for high-throughput
    globalCacheTTL: "5m"
    globalCacheMaxEntries: 10000
~~~

**Isolation guarantee**: Tokens are keyed by the full SHA-256 of the
caller's assertion. Different callers have different JWTs, so their cache
entries never collide. A caller can only retrieve a cached token if they
present the exact same bearer token that produced it.

### 5. No Authorization Model

The blueprint covers authentication (who is the caller?) and delegation
(how does their identity flow?) but is silent on authorization:

- Can any authenticated user run any solution?
- Can solutions be scoped to specific roles/groups?
- Can a caller run a solution that uses `env` provider but not `file`?
- The legacy API server extracts `roles` from the JWT -- are they enforced?

**Decision needed**: State explicitly that RBAC/authorization is out of
scope for this issue, or add a phase for it. At minimum, the `roles` claim
should be available for future use.

### 6. GitHub PAT in Request Body Is a Log Leak Vector

Accepting `githubToken` in JSON body means:

- Request body audit logging captures it
- Error responses echoing the request include it
- Debug/diagnostic dumps contain it

Headers are easier to redact in logging middleware. **Deprecate the body
field from day one.** Add redaction for `githubToken` in any audit/error
response path.

### 7. Delegation Config Defaults Must Be Safe

If `apiServer.delegation` is absent from config, what happens?

**Safe default**: All delegation features disabled. Solutions in API mode
cannot obtain caller tokens or perform OBO. This forces operators to
explicitly configure delegation. The alternative (delegation enabled with no
restrictions) is dangerous.

### 8. OBO Cache Key Truncation

Per-request cache uses `sha256(assertion)[0:16]` (64 bits). Fine for a
per-request scope (tiny keyspace). The global cache tier uses the full
SHA-256 hash. **Ensure both tiers are consistent** -- use the full hash
everywhere to avoid surprises if code is refactored.

### 9. Missing: Audit Trail for Token Exchanges

No mention of logging when the API mints OBO or client_credentials tokens:

- What scope was requested
- For which downstream API
- Which caller triggered it
- Cache hit or miss

This is critical for security incident response. **Add structured log
events for every token exchange.**

### 10. `__request.claims` Contains PII

Claims include email, display name, group memberships, and Azure object IDs.
This is PII that could end up in resolver output, state files, or diagnostic
dumps.

**Consider**: Filter `__request.claims` to expose only non-PII fields
(`sub`, `oid`, `roles`, `callerType`) by default. Full claims
(`email`, `name`, `groups`) available only with an opt-in flag (similar to
`exposeCallerToken`).

### 11. Phase Ordering -- Resolved

The implementation phases (A-E) are now structured so that restrictions and
sandboxing (Phase A) ship before any capability enablement (Phases B-D).
Downstream URL restriction ships in the same PR as OBO (Phase C). This
eliminates the security window concern.

Remaining ordering constraint to enforce during implementation: **never
merge a Phase C or D PR without Phase A and B already on `main`.**

## Related

- #376 -- expose request context to solution resolvers in API mode
- Legacy API server auth implementation
- `pkg/api/middleware/auth.go` -- existing OIDC middleware
- `pkg/auth/entra/obo.go` -- existing OBO implementation (unwired)
- `pkg/plugin/pool.go` -- existing plugin pool with allow/deny lists
- `docs/design/api-plugin-lifecycle.md` -- existing plugin lifecycle design

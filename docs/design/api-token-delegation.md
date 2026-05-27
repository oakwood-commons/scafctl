---
title: "Token Delegation"
weight: 6
---

# Token Delegation

## Implementation Status

| Feature | Status | Notes |
|---------|--------|-------|
| OBO flow (user -> downstream) | [done] Implemented | `pkg/authdelegation/flows.go` |
| Client credentials flow (machine -> downstream) | [done] Implemented | `pkg/authdelegation/flows.go` |
| WIF server credential | [done] Implemented | Projected federated token file |
| Secret server credential | [done] Implemented | Static client secret via `SecretRef` |
| LRU + TTL token cache | [done] Implemented | `pkg/authdelegation/tokencache.go` |
| Singleflight deduplication (go-flight Manager) | [done] Implemented | Prevents thundering herd |
| Flow registry (allow-list) | [done] Implemented | Only explicitly permitted flows execute |
| Cache key generation (SHA-256 hashing) | [done] Implemented | `pkg/authdelegation/keygen.go` |
| Delegator registry (multi-provider) | [done] Implemented | `pkg/authdelegation/registry.go` |
| OpenTelemetry tracing | [done] Implemented | Spans + events for cache hits/misses |
| TokenPassthrough middleware | [done] Implemented | Extracts configured `X-Authorization-*` headers into request context |
| PassThroughDelegator | [done] Implemented | Returns request-scoped provider tokens without exchange |
| HTTP provider delegator resolution | [done] Implemented | Exact `authProvider` lookup first, then `passThrough:<provider>` fallback |
| Passing delegated tokens downstream to providers | [done] Implemented | HTTP provider injects `Authorization: Bearer <token>` |
| Google / other IdP support | [planned] Future | Currently Entra-only |

---

## Problem Statement

When scafctl runs in **API mode**, incoming requests carry a caller's bearer token. Providers that execute on behalf of that caller often need a *different* token scoped to a downstream Azure resource (e.g., Azure Key Vault, Microsoft Graph).

Naively acquiring a new token on every provider invocation is:

1. **Slow** -- each token endpoint round-trip adds 100-500 ms of latency.
2. **Wasteful** -- identical scopes requested within the same token lifetime produce duplicate network calls.
3. **Fragile** -- concurrent requests for the same token can trigger rate limits at the identity provider.

Token delegation solves this by centralising the exchange logic, caching results, and deduplicating in-flight requests.

---

## Architecture

~~~
+------------------------------------------------------------------+
|                        API Server                                 |
|                                                                  |
|  +----------+     +-----------------+     +------------------+  |
|  | Auth     |---->| DelegatorRegistry|---->| EntraDelegator   |  |
|  | Middleware|     | (per-provider)  |     |                  |  |
|  |          |     +-----------------+     |  +------------+  |  |
|  | Extracts |                             |  |FlowRegistry|  |  |
|  | token +  |                             |  | obo        |  |  |
|  | claims   |                             |  | client_cred|  |  |
|  +----------+                             |  +------------+  |  |
|                                           |                  |  |
|                                           |  +------------+  |  |
|                                           |  | go-flight  |  |  |
|                                           |  | Manager    |  |  |
|                                           |  | (cache +   |  |  |
|                                           |  | singleflight)| |  |
|                                           |  +------------+  |  |
|                                           |                  |  |
|                                           |  +------------+  |  |
|                                           |  |ServerCred  |  |  |
|                                           |  |(WIF|Secret)|  |  |
|                                           |  +------------+  |  |
|                                           +------------------+  |
+------------------------------------------------------------------+
~~~

Pass-through delegation is intentionally separate from Entra token exchange. Entra delegation exchanges an incoming caller token for a new downstream token through OBO or client credentials. Pass-through delegation accepts a provider-specific token supplied on the API request and makes that same request-scoped token available to providers; it does not exchange, cache, or refresh it.

### Key Components

| Component | Responsibility |
|-----------|---------------|
| `TokenDelegator` (interface) | Top-level facade -- one method: `DelegateToken(ctx, scope)` |
| `EntraDelegator` | Concrete implementation for Azure Entra ID |
| `TokenPassthrough` middleware | Extracts allowed `X-Authorization-*` headers and stores tokens in request context by suffix |
| `PassThroughDelegator` | Implements `TokenDelegator` by returning a matching request-scoped pass-through token |
| `FlowRegistry` | Write-at-init map of permitted flow names -> `FlowFn` |
| `FlowFn` | Executes a single token endpoint request (OBO or client_credentials) |
| `ServerCredential` | Variability point for server proof-of-identity (WIF vs secret) |
| `DelegatorRegistry` | Maps provider names -> `TokenDelegator` instances (concurrent-safe), including non-passthrough delegators and internal `passThrough:<provider>` entries |
| `TokenCache` | LRU + TTL local cache wrapping `pkg/cache.LRUWithTTL` |
| `go-flight Manager` | Singleflight + cache orchestrator (dedup + stale-while-revalidate) |
| `KeyGenerator` | Deterministic cache key from `FlowParams` |

---

## Flow Diagrams

### On-Behalf-Of (User Caller)

~~~
User --bearer--> API Server
                    |
                    v
          Auth Middleware
          (extracts token + callerType="user")
                    |
                    v
          EntraDelegator.DelegateToken(ctx, scope)
                    |
                    +- key = "obo|{clientID}|{scope}|sha256({callerToken})"
                    v
          go-flight Manager.Do(key, ...)
                    |
          +--------+--------+
          | Cache hit?       |
          | Yes -> return     |
          | No / expired --> |
          +--------+--------+
                   v
          oboFlow(ctx, FlowParams)
                   |
                   v
          POST https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token
            grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer
            assertion={callerToken}
            scope={scope}
            requested_token_use=on_behalf_of
            + server credential (client_assertion or client_secret)
                   |
                   v
          TokenResult{AccessToken, ExpiresIn}
~~~

### Client Credentials (Machine Caller)

~~~
Service Principal --bearer--> API Server
                                |
                                v
                      Auth Middleware
                      (callerType="app")
                                |
                                v
                      EntraDelegator.DelegateToken(ctx, scope)
                                |
                                +- key = "cc|{clientID}|{scope}"
                                v
                      go-flight Manager.Do(key, ...)
                                |
                      +---------+---------+
                      | Cache hit? ...     |
                      +---------+---------+
                                v
                      clientCredentialFlow(ctx, FlowParams)
                                |
                                v
                      POST .../token
                        grant_type=client_credentials
                        scope={scope}
                        + server credential
~~~

### Pass-Through Token Flow

~~~
Client request
  Header: X-Authorization-Github: ghp_...
                    |
                    v
          TokenPassthrough middleware
          stores token in request context as "Github"
                    |
                    v
          DelegatorRegistry
          contains internal entry "passThrough:Github"
                    |
                    v
          HTTP provider input
          authProvider: Github
                    |
                    v
          resolveDelegator(...)
            1. registry lookup: "Github"
            2. fallback lookup: "passThrough:Github"
                    |
                    v
          PassThroughDelegator.DelegateToken(ctx, scope)
          returns request-scoped token from context
                    |
                    v
          Downstream request
          Authorization: Bearer ghp_...
~~~

Users configure provider inputs with the plain provider suffix, for example `authProvider: Github`. The `passThrough:` prefix is an internal registry namespace and should not be used in provider input.

---

## Caching Strategy (Detailed)

### Three Layers

1. **Key Generation** -- deterministic key per unique token exchange scenario.
2. **LRU + TTL local cache** (`TokenCache`) -- bounded by `cacheSize` (default 1024), entries expire per the token's `expires_in` minus an `expiryBuffer` (default 10 min).
3. **Singleflight deduplication** (`go-flight Manager`) -- concurrent requests for the same key block on a single in-flight fetch rather than issuing duplicate token endpoint calls.

### Key Generation

| Caller Type | Key Format | Hash Function |
|-------------|-----------|---------------|
| `user` (OBO) | `obo\|{clientID}\|{scope}\|sha256({callerToken})` | SHA-256 hex of JWT |
| `app` (client_credentials) | `cc\|{clientID}\|{scope}` | N/A -- no caller token |
| Unknown | No caching (NoOpKeyGenerator) | -- |

OBO keys include the token hash because the same scope requested by different users must produce different downstream tokens.

### go-flight Manager Behaviour

| Setting | Default | Effect |
|---------|---------|--------|
| `expiryThreshold` | 30 min | Tokens expiring within this window trigger background refresh |
| `slowThreshold` | 500 ms | Fetch exceeding this logs a warning |
| `retryOnFollowerError` | true | If the leader fetch fails, blocked followers retry independently |
| `cleanupInterval` | 5 min | How often the LRU evicts expired entries |
| `expiryBuffer` | 10 min | Subtracted from `expires_in` to avoid serving nearly-expired tokens |

### Cache Miss Lifecycle

~~~
Request arrives -> GenerateKey(callerType, params)
  +- key="" -> bypass cache, call flow directly
  +- key="obo|..." -> Manager.Do(key, fetchFn, hooks)
       +- Cached + fresh -> return immediately (OnCacheHit event)
       +- Cached + stale (within expiryThreshold) -> serve stale, refresh in background
       +- Not cached -> single leader executes fetchFn
            +- Success -> store with TTL = expires_in seconds, unblock followers
            +- Failure -> if retryOnFollowerError, followers retry independently
~~~

---

## Security Considerations

### Token Handling

- **Caller tokens are never stored in plaintext in cache keys.** OBO keys hash the JWT with SHA-256 before inclusion.
- **Token cache is in-memory only.** No persistence to disk -- tokens are lost on process restart.
- **Bounded cache size** prevents memory exhaustion from token accumulation.
- **TTL enforcement** ensures tokens are not served past their lifetime.
- **Pass-through tokens are request-scoped.** The middleware extracts only explicitly allowed headers and stores the resulting token map on the request context.
- **Pass-through tokens are not exchanged, cached, refreshed, or logged.** They are copied only into the downstream `Authorization: Bearer <token>` header when the matching provider is used.
- **Non-passthrough delegators take precedence over pass-through fallback.** If a registry entry exists for `Github`, it wins over the internal `passThrough:Github` entry.

### Server Credentials

| Type | Storage | Rotation |
|------|---------|----------|
| WIF (`WIFCredential`) | Projected token file on disk (Kubernetes) | Re-read on every request -- automatic rotation |
| Secret (`SecretCredential`) | Resolved once at startup from `SecretRef` URI (env var or file) | Requires restart to rotate |

- WIF is preferred in production -- short-lived, auto-rotated by the platform.
- Secrets must never be hardcoded; they are resolved via `config.SecretRef` which reads from environment variables or files at startup.

### Flow Allow-List

The `FlowRegistry` acts as a security boundary: only flows explicitly registered during startup can execute. If an admin configures `allowedFlows: [obo]`, a machine caller requesting `client_credentials` will receive an error rather than silently falling back.

### Response Validation

- Token endpoint responses are read with `io.LimitReader(resp.Body, 1<<20)` (1 MB cap) to prevent response-body DoS.
- Error responses are truncated to 512 bytes before inclusion in error messages to prevent log injection.

---

## Configuration

### YAML Reference

~~~yaml
apiServer:
  identity:
    entra:
      tenantId: "00000000-0000-0000-0000-000000000000"
      clientId: "00000000-0000-0000-0000-000000000000"
      credential:
        type: secret                                             # "wif" or "secret"
        clientSecret: "env://SCAFCTL_API_ENTRA_CLIENT_SECRET"  or file://<filepath>  # SecretRef URI (required when type=secret)
        wifTokenPath: "/var/run/secrets/azure/tokens/fed-token"  # required when type=wif
      allowedFlows:                  # optional -- see nil semantics below
        flows: [obo, client_credentials]
      tokenManager:                  # optional -- see nil semantics below
        cacheSize: 1024
        expiryBuffer: "10m"
        cleanupInterval: "5m"
        expiryThreshold: "30m"
        slowThreshold: "500ms"
        retryFollowerOnError: true
  tokenPassThrough:                  # optional -- see nil semantics below
    allowedHeaders:                  # suffixes only, without X-Authorization-
      - Github
      - Entra
~~~

### SecretRef Semantics

`SecretRef` is a **scalar URI string** (`type SecretRef string`), not a nested object. Two schemes are supported:

| Scheme | YAML Value | Resolution |
|--------|-----------|------------|
| `env://` | `"env://MY_CLIENT_SECRET"` | Reads `os.Getenv("MY_CLIENT_SECRET")` at startup; errors if empty or unset |
| `file://` | `"file:///etc/secrets/client-secret"` | Reads and trims the file contents; errors if empty or missing |

Any other scheme is rejected at validation time. The secret is resolved **once** during `NewEntraDelegatorFromConfig` and held in the `SecretCredential` struct in memory for the server lifetime.

### Nil-Pointer Semantics

The configuration uses nil-pointer semantics for key fields to distinguish "not configured" from "configured with defaults" from "configured with explicit values":

| Field | nil / omitted | Present but empty | Present with values |
|-------|---------------|-------------------|---------------------|
| `tokenManager` | **No caching, no singleflight dedup.** Every `DelegateToken` call hits the token endpoint directly. Suitable for low-traffic deployments or testing. | Uses all defaults: cache size 1024, expiry buffer 10m, cleanup 5m, expiry threshold 30m, slow threshold 500ms, retry on error = true. | Each field overrides its corresponding default; zero-value numbers fall back to defaults. |
| `allowedFlows` | **Only OBO is permitted** (the safe default). Machine callers (`callerType="app"`) receive error: `"delegation flow \"client_credentials\" is not permitted"`. | **All flows denied.** `flows: []` means nothing is in the allow-list -- both OBO and client_credentials are rejected. This is a deny-all posture. | Only the explicitly listed flows are registered in the `FlowRegistry`. Unrecognized flow names fail validation at startup. |
| `tokenPassThrough` | **Default pass-through header enabled.** Omitted config allows `X-Authorization-Github` and registers `passThrough:Github`. | **Pass-through disabled.** `allowedHeaders: []` registers no internal pass-through delegators and extracts no X-Authorization headers. | Each suffix registers one internal pass-through delegator named `passThrough:<suffix>` and allows request header `X-Authorization-<suffix>`. |

### Token Pass-Through Semantics

`apiServer.tokenPassThrough.allowedHeaders` contains header suffixes only. Values must not include the `X-Authorization-` prefix.

~~~yaml
apiServer:
  tokenPassThrough:
    allowedHeaders:
      - Github
      - Entra
~~~

For the `Github` suffix:

~~~
X-Authorization-Github: ghp_...
authProvider: Github

authProvider "Github"
  -> normal registry lookup: Github
  -> fallback registry lookup: passThrough:Github
  -> PassThroughDelegator returns token
Authorization: Bearer ghp_...
~~~

The configured suffix is also the key used in request context. Header validation rejects empty suffixes, suffixes that already include `X-Authorization-`, and values that would produce an invalid HTTP header name.

### Decision Table: `allowedFlows` Behaviour

~~~
allowedFlows field                          | User (OBO) | Machine (client_credentials)
--------------------------------------------+------------+-----------------------------
omitted (nil)                               | [done] allowed  | [no] denied
present, flows: []                          | [no] denied   | [no] denied
present, flows: [obo]                       | [done] allowed  | [no] denied
present, flows: [client_credentials]        | [no] denied   | [done] allowed
present, flows: [obo, client_credentials]   | [done] allowed  | [done] allowed
~~~

### Decision Table: `tokenManager` Behaviour

~~~
tokenManager field         | Caching          | Singleflight dedup | Background refresh
---------------------------+------------------+--------------------+--------------------
omitted (nil)              | [no] off            | [no] off              | [no] off
present (zero values)      | [done] on (defaults)  | [done] on               | [done] on
present (custom values)    | [done] on (custom)    | [done] on               | [done] on
~~~

### Credential Validation at Startup

`ServerCredentialConfig.Validate()` enforces:

- `type` is required -- must be `"wif"` or `"secret"`
- When `type: secret` -> `clientSecret` must be a non-empty, valid `SecretRef` (validated scheme + non-empty value)
- When `type: wif` -> `wifTokenPath` must be non-empty (file existence is checked at first use, not startup)

`DelegationFlowsConfig.Validate()` enforces:

- Every entry in `flows` must be one of: `"obo"`, `"client_credentials"`
- Unrecognized flow names fail with: `invalid flow "X", must be one of: obo, client_credentials`

---

## Future Work

| Item | Notes |
|------|-------|
| **Google Cloud delegation** | Same `TokenDelegator` interface, different flow implementation (STS token exchange or impersonation) |
| **Configurable delegator priority/fallback policy** | Allow admins to choose whether pass-through can override normal delegators, remain fallback-only, or be disabled per provider |
| **Certificate-based credential** | `ServerCredential` implementation using X.509 client cert assertion |
| **Multi-tenant delegation** | Per-tenant `EntraDelegator` instances selected by incoming token's `tid` claim |
| **Metrics** | Prometheus counters for cache hit/miss ratio, flow latency percentiles, error rates |
| **Token exchange (RFC 8693)** | Generic token exchange flow for cross-IdP scenarios |

---

## Package Layout

~~~
pkg/authdelegation/
+-- context.go          # Registry stored/retrieved from context
+-- credentials.go      # ServerCredential interface + WIF/Secret impls
+-- delegator.go        # EntraDelegator + functional options + DelegateToken
+-- errors.go           # Sentinel errors
+-- flows.go            # FlowFn, OBO/client_credentials flows, FlowRegistry
+-- keygen.go           # Key generators + SHA256Hash
+-- passthrough.go      # PassThroughDelegator + internal passThrough:<provider> naming
+-- registry.go         # DelegatorRegistry (concurrent-safe name->delegator map)
+-- tokencache.go       # TokenCache[K,V] wrapping pkg/cache.LRUWithTTL
~~~

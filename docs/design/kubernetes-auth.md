---
title: "Kubernetes / OpenShift Authentication"
---

# Kubernetes / OpenShift Authentication

## Overview

This document describes scafctl's first-class Kubernetes / OpenShift
authentication design (issue #536). The goal is to let any scafctl auth handler
serve tokens to `kubectl` / `oc` (and any kubeconfig-aware tool) while keeping
the **core dependency-light** -- no `client-go` in core. Heavier machinery lives
in plugins.

The design is layered by dependency weight so each phase ships independently:

| Phase | Scope | Location | Status |
|-------|-------|----------|--------|
| 1a | Handler-agnostic exec-credential output on `auth token` | core (`pkg/auth/execcredential`) | Shipped |
| 1b | `ClusterResolver` interface + `RootOptions` hook | core (`pkg/kube`) | Shipped |
| 2a | Kubeconfig provider capability contract + host-side manager | core (`pkg/kubeconfig`, `CapabilityKubeconfig`) | Shipped |
| 2b | Kubeconfig provider plugin implementation (`client-go`/`clientcmd`) | plugin ([`scafctl-plugin-kubeconfig`](https://github.com/oakwood-commons/scafctl-plugin-kubeconfig)) | Shipped |
| 3 | Thin `kube login` / `kube logout` commands | core (`pkg/kube/login`, `pkg/cmd/scafctl/kube`) | Shipped |
| 4 | OpenShift OAuth auth-handler plugin + two-path routing | plugin + core (`pkg/kube/login`, `pkg/catalog`, `pkg/auth/official`) | Shipped |
| 5 | Multi-cluster per-cluster token routing on the exec-credential path | core (`pkg/auth/execcredential`, `pkg/cmd/scafctl/auth`, `pkg/kube/login`) + SDK `CapTokenHostname` | Shipped |

Phases 1-3 add no OpenShift- or vendor-specific code and already deliver
credential-helper support for any OIDC cluster.

## Phase 1a: Exec-Credential Output (Shipped)

`auth token <handler> --exec-credential` emits a
`client.authentication.k8s.io/v1` `ExecCredential` (`status.token`,
`status.expirationTimestamp`) built from the token already returned by
`GetToken`. The payload is a hand-rolled struct -- no `client-go` import. Output
is auto-enabled when kubectl sets `KUBERNETES_EXEC_INFO`, so the kubeconfig exec
command needs no special flag.

See the [auth tutorial](../tutorials/auth-tutorial.md) for the kubeconfig wiring.

## Phase 1b: ClusterResolver (Shipped)

The `pkg/kube` package defines a dependency-free extension point that maps a
cluster name to its connection details. The stock binary can populate it from
configuration (see Config-Driven Resolution below); embedders with their own
cluster registry can supply an implementation instead.

### The Interface

~~~go
package kube

type AuthType string

const (
    AuthTypeAuto  AuthType = ""      // auto-detect
    AuthTypeOAuth AuthType = "oauth" // OpenShift bundled OAuth server
    AuthTypeOIDC  AuthType = "oidc"  // external OIDC identity provider
)

type ClusterInfo struct {
    Name            string
    APIServerURL    string
    ConsoleURL      string
    AuthType        AuthType
    OIDCAudience    string
    DefaultHandler  string
    CAData          string
    InsecureSkipTLS bool
}

type ClusterResolver interface {
    Resolve(ctx context.Context, name string) (*ClusterInfo, error)
    List(ctx context.Context) ([]ClusterInfo, error) // powers completion
}
~~~

### ClusterInfo Fields

| Field | Purpose |
|-------|---------|
| `Name` | Cluster's logical name used for lookup and completion. |
| `APIServerURL` | API server endpoint. May be empty for `--server` or auto-detection. |
| `ConsoleURL` | Optional web console URL (informational). |
| `AuthType` | Authentication method; empty means auto-detect. |
| `OIDCAudience` | Client ID / audience the minted token must target for OIDC clusters. |
| `DefaultHandler` | Auth handler used when the caller omits `--handler`. Lets users run `kube login <cluster>` without naming a handler. Optional. |
| `CAData` | PEM-encoded cluster CA bundle. When set, login pins the API server to this CA instead of falling back to `InsecureSkipTLS`. |
| `InsecureSkipTLS` | Disables API server TLS verification (development only). |

`ClusterInfo.Validate()` rejects an empty `Name` (`ErrEmptyClusterName`) or an
unrecognized `AuthType` (`ErrInvalidAuthType`). `APIServerURL` is intentionally
optional because login can resolve it via `--server` or auto-detection.

### Handler Resolution

The auth handler is chosen after the cluster is resolved, using the precedence
`--handler` flag -> resolved `ClusterInfo.DefaultHandler` -> error
(`ErrNoHandler`). The CLI passes `auth.GetHandler` to the login orchestration as
a `Deps.HandlerLookup` hook so the domain layer owns the precedence and the
command stays thin. This lets an embedder's cluster registry supply the default
handler so users can run `kube login <cluster>` with no flags.

`kube login --verify` requires a successful post-login `whoami`; when the
kubeconfig provider is unavailable (static-fallback path) verification cannot run
and `--verify` fails by design. Without `--verify`, `whoami` remains best-effort
and only populates the reported identity.

### Embedder Hook

Embedders supply a resolver through `RootOptions.ClusterResolver`. When set, it
is attached to the command context and retrievable with
`kube.ResolverFromContext(ctx)`:

~~~go
opts := &scafctl.RootOptions{
    BinaryName:      "mycli",
    ClusterResolver: myClusterRegistry, // implements kube.ClusterResolver
}
cmd, cleanup := scafctl.Root(opts)
defer cleanup()
~~~

When unset, `kube.ResolverFromContext` returns `nil` and cluster-aware commands
fall back to an explicit `--server` and auto-detection. Setting a resolver lights
up positional cluster names, shell completion (via `List`), and OIDC audience
resolution.

### Config-Driven Resolution (Shipped)

The stock binary resolves clusters by name from the `kube.clusters` config
section, so users get positional `kube login <cluster>` without an embedder
resolver. `pkg/kube/clusterconfig` implements `kube.ClusterResolver` backed by
config and is attached by `RootOptions` when no embedder resolver is supplied
and `kube.clusters` declares any aliases or an inventory. An embedder-provided
`ClusterResolver` always wins.

Resolution precedence, highest to lowest:

1. **Explicit flags** -- `--server` / `--handler` override everything.
2. **Concrete URL argument** -- a `<cluster>` that is already an `http(s)://`
   URL is used directly as the API server (no inventory required) and is not
   used as the logical name.
3. **Static alias** -- a `kube.clusters.aliases.<name>` entry carrying `server`
   plus optional `defaultHandler`, `authType`, `oidcAudience`, `caData`.
4. **Dynamic inventory** -- `kube.clusters.resolver` fetches and CEL-transforms
   a cluster inventory. It reuses the auth hostname inventory engine
   (`pkg/auth/hostname`: fetch + transform + TTL cache); the transform yields
   `{name, url}` entries plus the same optional per-cluster fields. Static
   aliases win over inventory entries of the same name.

~~~yaml
kube:
  clusters:
    aliases:
      lab:
        server: https://api.lab.example.com:6443
        defaultHandler: openshift
    resolver:
      source:
        url: https://clusters.example.com/
      transform: '_.map(k, {"name": k, "url": _[k].apiServerURL, "defaultHandler": "openshift", "authType": "oauth"})'
      ttl: 10m
~~~

Because a fleet transform can stamp the same `defaultHandler` and `authType` on
every entry, `kube login <cluster>` then needs neither `--server` nor
`--handler`. One-off users with only a URL or a handful of static aliases keep
working unchanged.

### Runtime Model

Cluster details are resolved **once at login** and baked into the kubeconfig
exec args, never resolved on every `kubectl` call. Resolving on each call would
run the embedder's lookup inside a non-interactive subprocess (latency plus a
hard runtime dependency). The runtime helper then mints a token for fixed args
with no resolver involved.

## Phases 2-4

- **Phase 2a -- Kubeconfig provider capability contract (Shipped).** The
  in-core `CapabilityKubeconfig` provider capability plus the host-side driver
  in `pkg/kubeconfig` (typed inputs/outputs, `Manager`, mock, official-provider
  registration). Defines the six operations dispatched on the `operation`
  input -- `kubeconfig_write`, `kubeconfig_remove`, `current_server`,
  `detect_auth_type`, `reachable`, `whoami` -- each returning `success: boolean`.
  No `client-go` enters core; the manager resolves and drives the external
  plugin over the existing provider RPC.
- **Phase 2b -- Kubeconfig provider plugin implementation (Shipped).** Lives in
  the [`scafctl-plugin-kubeconfig`](https://github.com/oakwood-commons/scafctl-plugin-kubeconfig)
  repository. Carries `client-go`/`clientcmd`. Merges/writes the kubeconfig
  cluster, user, and context with an `ExecConfig` credential plugin; implements
  `DetectAuthType`, `CheckAPIServerReachable` (`/healthz`), and `Whoami`
  (SelfSubjectReview). Vendor-neutral; reused by both OIDC and OpenShift paths.
- **Phase 3 -- `kube login` / `kube logout` commands (Shipped).**
  Orchestration only, in `pkg/kube/login` (domain) with thin CLI wiring in
  `pkg/cmd/scafctl/kube`: run the handler login to mint a token, then delegate
  the kubeconfig write to the Phase 2b provider over the existing provider RPC.
  Surfaced under a dedicated `kube` command group as `kube login [cluster]
  [--handler <name>]` and `kube logout [cluster]`. Consumes
  `kube.ClusterResolver` to resolve a cluster name into `--server`,
  `--audience`, and a default handler (so `--handler` is optional when the
  resolver supplies `ClusterInfo.DefaultHandler`). When the kubeconfig provider
  plugin cannot be fetched, it degrades gracefully to writing a minimal static
  exec-credential kubeconfig directly. `client-go` never enters core.
- **Phase 4 -- OpenShift OAuth handler plugin + two-path routing (Shipped).**
  The one genuinely OpenShift-specific credential source: the
  `openshift` auth-handler plugin runs a localhost-callback implicit-grant flow
  (OAuth server discovered from the cluster's
  `.well-known/oauth-authorization-server` endpoint), enriches the username via
  `user.openshift.io`, and mints service-account tokens through the Kubernetes
  TokenRequest API -- all over raw HTTP, so no `client-go` enters core. The
  plugin is registered in `pkg/auth/official` so `kube login` auto-fetches it
  from `ghcr.io/oakwood-commons/auth-handlers/openshift`.

  **Two-path routing.** `kube login` auto-detects the cluster's auth method via
  the kubeconfig provider's `DetectAuthType` (`pkg/kube/login`) and, when the
  caller supplies neither `--handler` nor a resolved
  `ClusterInfo.DefaultHandler`, routes on the detected `AuthType`:

  - `oauth` (OpenShift bundled OAuth) -> the `openshift` handler's browser
    web-login flow.
  - `oidc` (structured / external identity, e.g. Entra) -> the existing `entra`
    handler with the cluster's audience. The audience comes from the resolver's
    `oidcAudience`, a `--audience` flag, or a static alias -- never a hardcoded
    tenant or client ID. When the OIDC route is auto-selected but no audience is
    resolvable, login fails fast (`ErrNoAudience`) rather than writing a
    kubeconfig entry that cannot authenticate.

  The policy is data, not a hardcoded switch: `login.Deps.AuthTypeHandlers`
  (defaulted by `login.DefaultAuthTypeHandlers`) maps an `AuthType` to a handler
  name and is consulted only as a last resort -- an explicit `--handler` and a
  resolver-supplied `DefaultHandler` both take precedence, so embedders that ship
  different handlers (or stamp `defaultHandler` in their cluster inventory)
  override the fallback without touching core. An undetected `AuthType` (auto)
  still requires an explicit handler.

  **Credential-helper use.** Once logged in, the OpenShift integrated image
  registry is served through the Docker credential-helper protocol
  (`pkg/credentialhelper`): `pkg/catalog` infers the `openshift` handler for the
  registry route (`default-route-openshift-image-registry.*`) and in-cluster
  service (`image-registry.openshift-image-registry.svc*`) host forms, so
  `docker`/`podman` pulls fetch fresh tokens from scafctl. Custom or renamed
  registry routes are supported via a `customOAuth2` registry mapping.

## Phase 5: Multi-Cluster Token Routing (Shipped)

A single auth handler can back several clusters. Because the exec-credential
kubeconfig entry `kube login` writes is otherwise identical for every cluster,
the exec helper needs a per-invocation discriminator so `kubectl`/`oc` receive
the correct cluster's token rather than the handler's most-recent one.

The mechanism uses the client-go-idiomatic cluster hint, gated on a dedicated
capability so mixed-version handlers never misroute silently (see issue #581 and
the gating decision in #583):

1. **`kube login` sets `provideClusterInfo: true`** on the kubeconfig exec
   block (`pkg/kube/login`). `kubectl`/`oc` then pass the target cluster's
   details -- including `spec.cluster.server` -- to the plugin via the
   `KUBERNETES_EXEC_INFO` environment variable on every credential call. The
   exec args stay cluster-agnostic (identical for all clusters); the cluster is
   supplied at call time.
2. **`auth token --exec-credential` extracts the server** from
   `KUBERNETES_EXEC_INFO` (`execcredential.ClusterServerFromExecInfo`) and
   forwards it as `TokenOptions.Hostname`, **only when the handler advertises
   `auth.CapTokenHostname`**. Handlers that advertise `CapHostname` for login
   but predate token-path hostname support never receive the field (proto3
   would silently drop it), so they cannot misroute.
3. **The host threads `Hostname`** through the in-process handler call and, for
   plugin handlers, across the gRPC boundary
   (`plugin.TokenRequest.Hostname` -> `proto.GetTokenRequest.hostname`) on both
   the client and server mappings.
4. **The routing key is the raw API server URL.** `kube login` sends
   `LoginRequest.Hostname = info.APIServerURL`, writes that same value as the
   kubeconfig `cluster.server`, and client-go echoes it back verbatim in
   `KUBERNETES_EXEC_INFO`. So the token-time hostname matches the login-time
   key by construction -- no normalization is required, and the handler stores
   and retrieves per-cluster tokens under one stable key.

When the resolved handler lacks `CapTokenHostname`, `kube login` emits a warning
at login time (not token time, where the message would be swallowed inside the
`kubectl` subprocess) noting that multi-cluster logins with that handler may
return the wrong token. Single-cluster usage and handlers without the capability
behave exactly as before.

## Design Decisions

- **No `client-go` in core.** It lives only in the Phase 2b kubeconfig provider
  plugin (and an optional shared library), never in `pkg/kube`.
- **No cluster data in core.** Cluster resolution is an embedder concern behind
  `ClusterResolver`.
- **`kube` package naming.** Every `ClusterInfo` field is Kubernetes-API-server
  specific, so the package is named for the `kube` domain rather than a generic
  `cluster` name.
- **CLI-only surface (no API / MCP exposure).** `kube login` and `kube logout`
  are intentionally not exposed through the HTTP API server or the MCP tool set.
  Both run interactive credential flows (browser redirect / device code) and
  mutate machine-local state -- the user's kubeconfig and the credential cache.
  Driving them from a remote API server or an MCP host would run the auth flow
  and write kubeconfig on the wrong machine. This mirrors the existing auth
  surface: the MCP `auth_*` tools are read-only inspection (`auth_status`,
  `list_auth_handlers`, `auth_list_tokens`, `auth_purge_expired`) with no
  `login` tool, and the API server exposes only solution/catalog/schema
  endpoints. Coverage therefore lives in CLI integration tests
  (`tests/integration/cli_test.go`) and package unit tests, with no
  `api_test.go` or `pkg/mcp/tools_*.go` counterparts by design.

## Resolved Open Questions

The four open questions from issue #536 are resolved as follows. These shape
Phases 2-4.

### Q1 -- Command Placement

Surface kube login under a dedicated `kube` command group as `kube login
[cluster] --handler <name>` (plus matching `kube logout [cluster]`), rather than
nesting it under the auth group or placing it at the top level. The handler is
the credential source (selected with `--handler`); a positional cluster name (or
`--server`) triggers the kube wiring and kubeconfig write. Grouping cluster
operations under `kube` keeps the "log into a cluster" workflow discoverable,
names the feature for the Kubernetes domain it serves, and leaves room for
sibling commands (`kube list`, `kube current`, `kube whoami`) without colliding
with the identity-oriented `auth login`.

> Note: earlier drafts proposed folding this into `auth login <handler>
> --cluster <name>`, then dedicated top-level `login` / `logout` commands. The
> shipped design uses a `kube` command group instead: `kube` is the honest name
> (every `ClusterInfo` field is Kubernetes-specific), it mirrors the `pkg/kube`
> package boundary, and it avoids two competing "login" verbs at different
> levels (`login` vs `auth login`).

### Q2 -- Handler Granularity

Keep single-purpose handlers; do not build a branching meta-handler. Reuse the
existing handlers (`gcp`, `entra`, `github`, and a future generic `oidc`
handler) for OIDC clusters, and add exactly one `openshift` OAuth handler as a
Phase 4 plugin for the bundled-OAuth case. Selection happens at the command
level via `ClusterInfo.AuthType` plus the provider's `DetectAuthType`, not
inside a handler.

### Q3 -- Thin Command vs Embedder-Provided

The kube wiring lives in core (so every embedder gets it) and delegates all
`client-go` work to the Phase 2b kubeconfig provider over gRPC. The provider is
auto-fetched on demand via the existing plugin `Fetcher`; if it cannot be
fetched, core degrades gracefully to `auth token <handler> --exec-credential`
for manual kubeconfig wiring (which already works without any plugin). The
provider RPC surface is roughly `WriteKubeconfig`, `RemoveManagedEntries`,
`GetCurrentContextServer`, `DetectAuthType`, `CheckAPIServerReachable`, and
`Whoami`.

### Q4 -- Token Cache

Reuse the existing auth keyring `TokenCache`; do not add a kube-specific cache.
The exec-credential path already reads from it, so a second cache would create
two sources of truth. The Phase 2b provider stays stateless and never handles
tokens. The resolved OIDC audience is folded into the cache-key scope dimension
so audience-specific tokens are distinct entries. Kube logout clears the keyring
(existing path) and additionally removes the managed kubeconfig entry.

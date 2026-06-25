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
| 2b | Kubeconfig provider plugin implementation (`client-go`/`clientcmd`) | plugin | Planned |
| 3 | Thin `login` / `kube` command | core | Planned |
| 4 | OpenShift OAuth auth-handler plugin | plugin | Planned |

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
cluster name to its connection details. scafctl ships **no** cluster data --
embedders with a cluster registry provide the implementation.

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
| `InsecureSkipTLS` | Disables API server TLS verification (development only). |

`ClusterInfo.Validate()` rejects an empty `Name` (`ErrEmptyClusterName`) or an
unrecognized `AuthType` (`ErrInvalidAuthType`). `APIServerURL` is intentionally
optional because login can resolve it via `--server` or auto-detection.

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
- **Phase 2b -- Kubeconfig provider plugin implementation (Planned).** Carries
  `client-go`/`clientcmd`. Merges/writes the kubeconfig cluster, user, and
  context with an `ExecConfig` credential plugin; implements `DetectAuthType`,
  `CheckAPIServerReachable` (`/healthz`), and `Whoami` (SelfSubjectReview).
  Vendor-neutral; reused by both OIDC and OpenShift paths.
- **Phase 3 -- Kube wiring on `auth login` / `auth logout`.** Orchestration
  only: run the handler login to mint a token, then delegate the kubeconfig
  write to the Phase 2b provider over the existing provider RPC. Surfaced as
  `auth login <handler> --cluster <name>` (and matching `auth logout`) rather
  than a new top-level command. Consumes `kube.ClusterResolver` to resolve a
  cluster name into `--server` and `--audience`. `client-go` never enters core.
- **Phase 4 -- OpenShift OAuth handler plugin.** The one genuinely
  OpenShift-specific credential source: localhost-callback implicit-grant flow,
  plus `ListProjects` / MOTD behind graceful degradation.

## Design Decisions

- **No `client-go` in core.** It lives only in the Phase 2b kubeconfig provider
  plugin (and an optional shared library), never in `pkg/kube`.
- **No cluster data in core.** Cluster resolution is an embedder concern behind
  `ClusterResolver`.
- **`kube` package naming.** Every `ClusterInfo` field is Kubernetes-API-server
  specific, so the package is named for the `kube` domain rather than a generic
  `cluster` name.

## Resolved Open Questions

The four open questions from issue #536 are resolved as follows. These shape
Phases 2-4.

### Q1 -- Command Placement

Fold kube login into the existing auth group as
`auth login <handler> --cluster <name>` (plus matching `auth logout`), rather
than adding a top-level `login` command. The handler is the credential source
(unchanged); `--cluster <name>` triggers the kube wiring. Without `--cluster`,
`auth login` behaves exactly as before.

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

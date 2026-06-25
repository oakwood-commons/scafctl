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
| 2 | Kubeconfig provider plugin (`client-go`/`clientcmd`) | plugin | Planned |
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

## Phases 2-4 (Planned)

- **Phase 2 -- Kubeconfig provider plugin.** Carries `client-go`/`clientcmd`.
  Merges/writes the kubeconfig cluster, user, and context with an `ExecConfig`
  credential plugin; provides `DetectAuthType`, `CheckAPIServerReachable`
  (`/healthz`), and `Whoami` (SelfSubjectReview). Vendor-neutral; reused by both
  OIDC and OpenShift paths.
- **Phase 3 -- Thin `login` / `kube` command.** Orchestration only: run
  `handler.Login` to mint a token, then delegate the kubeconfig write to the
  Phase 2 provider over the existing provider RPC. Consumes
  `kube.ClusterResolver` to resolve a positional cluster name into `--server`
  and `--audience`. `client-go` never enters core.
- **Phase 4 -- OpenShift OAuth handler plugin.** The one genuinely
  OpenShift-specific credential source: localhost-callback implicit-grant flow,
  plus `ListProjects` / MOTD behind graceful degradation.

## Design Decisions

- **No `client-go` in core.** It lives only in the Phase 2 kubeconfig provider
  plugin (and an optional shared library), never in `pkg/kube`.
- **No cluster data in core.** Cluster resolution is an embedder concern behind
  `ClusterResolver`.
- **`kube` package naming.** Every `ClusterInfo` field is Kubernetes-API-server
  specific, so the package is named for the `kube` domain rather than a generic
  `cluster` name.

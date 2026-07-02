---
title: "Kubeconfig Provider Implementation Plan (Phase 2)"
---

# Kubeconfig Provider Implementation Plan (Phase 2)

## 1. Summary

Phase 2 of issue #536 builds the **kubeconfig provider** -- a go-plugin gRPC
binary, carried in its own module, that owns all `client-go`/`clientcmd` work so
core never imports the heavy Kubernetes client packages. It is auto-fetched on
demand and performs the Kubernetes-side mechanics (merge/write a kubeconfig
exec-credential entry, remove it, read the current server, detect oauth-vs-oidc,
check reachability, and run a SelfSubjectReview whoami). A new in-core host-side
manager package (`pkg/kubeconfig`) drives it, mirroring `pkg/state`. The provider
is stateless -- it never stores or caches tokens (Q4) and receives a token only
for `whoami`. The Phase 3 `auth login <handler> --cluster` command and the Phase
4 OpenShift handler consume this provider; both are out of scope here.

The work is split into Phase 2a (in-core contract + manager + mock, no
client-go) and Phase 2b (the external plugin module with client-go).

## 2. Architecture Decisions

### Layers affected

- **provider** -- new scafctl-defined capability `CapabilityKubeconfig`.
- **new package `pkg/kubeconfig`** -- host-side manager (direct analogue of
  `pkg/state/manager.go`).
- **provider/official** -- register `kubeconfig` for auto-fetch.
- **external module** (separate repo) -- the plugin carrying client-go.

No CLI/MCP/API changes in Phase 2 (those land in Phase 3).

### Mirror the state backend

The state backend is the exact precedent for invoking a provider directly from a
domain package outside a solution DAG:

- `pkg/state/manager.go` resolves a provider via `registry.Get`, checks it has
  `CapabilityState`, calls `provider.Execute` with an `operation` input
  (`state_load`/`state_save`/`state_delete`), and extracts `Output.Data`.
- The provider dispatches on the `operation` field
  (`pkg/provider/builtin/httpprovider/http_state.go`).

The kubeconfig provider reuses this shape with operation dispatch on an
`operation` input field, and a host-side manager that resolves, checks the
capability, executes, and unmarshals typed output.

~~~text
auth login <handler> --cluster X        (Phase 3, core, no client-go)
        |
        v
pkg/kubeconfig.Manager                   (Phase 2a, core -- mirrors pkg/state.Manager)
        | ensure provider registered (fetch-then-register; see section 3)
        | registry.Get("kubeconfig"); check CapabilityKubeconfig
        | Execute(operation=kubeconfig_write, inputs=...)
        v
kubeconfig provider plugin               (Phase 2b, external module, carries client-go)
        | clientcmd merge/write, rest /healthz, SelfSubjectReview
        v
~/.kube/config  +  detection/whoami results
~~~

### Capability: `CapabilityKubeconfig`, not `CapabilityAction` (recommended)

Recommendation: a dedicated `CapabilityKubeconfig`, defined in `pkg/provider`
(not the SDK), exactly like `CapabilityState`.

Rationale:

- **Clean discovery + validation.** `ValidateDescriptor` already special-cases
  scafctl-defined capabilities (it strips them before the SDK validator and
  validates required output fields locally). A dedicated capability lets the
  manager assert the right provider type via `registry.ListByCapability` /
  capability check, exactly as state does. Reusing `CapabilityAction` would make
  any action provider falsely appear as a kubeconfig backend.
- **Mirrors an established, merged pattern** (`CapabilityState`), so reviewers and
  embedders already understand it; external plugins can implement it too.
- **Cost is small.** It is one constant plus a required-output-fields entry in
  `ValidateDescriptor`, copying the `stateCapabilityRequiredFields` approach.

Trade-off: `CapabilityAction` would avoid touching `ValidateDescriptor`, but at
the cost of weaker typing and discovery. The state precedent settles this in
favor of a dedicated capability.

### Write-operation guard exemption (issue #579)

The host-side write-operation guard (`provider.ValidateWriteOperation`) rejects
declared write operations only in the **read-only resolver phases**
(`CapabilityFrom`, `CapabilityTransform`, `CapabilityValidation`). Write-capable
capabilities -- `CapabilityKubeconfig`, `CapabilityState`, `CapabilityAction`,
and `CapabilityAuthentication` -- are exempt, so a provider may classify
`kubeconfig_write`/`kubeconfig_remove` as write operations without those calls
being blocked when the manager runs them under `CapabilityKubeconfig`.

Regression note: an earlier guard whitelisted only `CapabilityAction`, which
blocked `kubeconfig_write` under `CapabilityKubeconfig` and broke `kube login`.
When adding a new write-capable capability, ensure it is *not* treated as a
read-only resolver phase by the guard.

## 3. The host invocation path and the registry/auto-fetch resolution

### Finding (the flagged risk, resolved)

The state manager assumes the provider is **already** in the registry because
state runs *inside* a solution execution, where the plugin pool has already run
`Ensure`/`Adopt` to populate the registry. A command-initiated
`auth login --cluster` call has **no solution and no pool pass**, so nothing
pre-populates the registry. Therefore the kubeconfig manager needs a thin
**fetch-then-register** step -- it cannot assume a populated registry.

That step already exists as a reusable precedent: `autoResolveProviderByName`
in `pkg/cmd/scafctl/run/common.go`, used by `run provider <name>` (also a
command-initiated, non-solution call). It does exactly:

1. `official.RegistryFromContext(ctx)` -> look up the official entry by name.
2. `prepare.BuildPluginFetcher(ctx)` -> build a `plugin.Fetcher`.
3. `fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)` where
   `dep = entry.ToPluginDependency()`.
4. `plugin.RegisterFetchedPlugins(ctx, reg, results, pluginCfg, clientOpts...)`
   -> wraps each provider and `registry.Register`s it; returns `[]*plugin.Client`.

Crucially, `RegisterFetchedPlugins` returns the spawned `*plugin.Client`s, and
**the caller owns their lifecycle and must `Kill()` them** when done. State never
deals with this because the pool owns plugin lifecycle there.

### Resolution

`pkg/kubeconfig.Manager` performs its own fetch-then-register, modeled on
`autoResolveProviderByName`, and returns a cleanup func that kills the clients.
Concretely the manager's `ensure` step:

1. If `registry.Get("kubeconfig")` already returns a provider (e.g. a solution
   run pre-loaded it, or a test injected a mock), use it as-is.
2. Else look up `kubeconfig` in `official.RegistryFromContext(ctx)`, build the
   fetcher via `prepare.BuildPluginFetcher(ctx)`, `FetchPlugins`, then
   `RegisterFetchedPlugins` into a manager-owned `provider.Registry`. Stash the
   returned clients so `Manager.Close()` kills them.

### Graceful fallback (Q3)

Any failure in ensure/fetch/execute returns a sentinel
(`ErrProviderUnavailable`) so the Phase 3 command can fall back to writing a
kubeconfig that shells out to `<host-binary> auth token <handler>
--exec-credential` directly. The manager exposes this as a typed error; the
fallback policy itself lives in Phase 3, not in the manager.

### Lifecycle note

Because the manager spawns plugin clients for a one-shot command, it must:

- spawn lazily (only when an operation is actually invoked),
- reuse a single client across all operations in one command invocation, and
- `Kill()` on `Close()` (deferred by the Phase 3 command).

This is the one material difference from `pkg/state` and is called out as a sub-
decision in section 12.

## 4. The capability and operation contract

All operations dispatch on the `operation` input field. Inputs/outputs are flat
`map[string]any` to match the SDK `ExecuteProvider(name, input) -> Output{Data}`
signature. The manager marshals typed structs to maps (JSON round-trip, like
`state.structToMap`) and unmarshals `Output.Data` back into typed results.

| operation          | Inputs (key fields)                                                                                                   | Output (Data fields)                          |
|--------------------|---------------------------------------------------------------------------------------------------------------------|-----------------------------------------------|
| `kubeconfig_write` | `server`, `audience`, `cluster_name`, `context_name`, `user_name`, `kubeconfig_path`, `exec_command`, `exec_args`, `ca_data`, `interactive_mode`, `install_hint`, `provide_cluster_info`, `insecure_skip_tls`, `set_current_context` | `success`, `context_name`, `kubeconfig_path`  |
| `kubeconfig_remove`| `cluster_name`, `context_name`, `user_name`, `kubeconfig_path`                                                        | `success`, `removed`                          |
| `current_server`   | `kubeconfig_path`, `context_name`                                                                                    | `server`                                      |
| `detect_auth_type` | `server`, `insecure_skip_tls`                                                                                        | `auth_type` (auto/oauth/oidc), `oidc_issuer`, `oauth_endpoint` |
| `reachable`        | `server`, `insecure_skip_tls`                                                                                        | `reachable` (bool), `status` (int)            |
| `whoami`           | `server`, `token`, `audience`, `insecure_skip_tls`                                                                   | `username`, `groups` (list), `uid`            |

Required output field across all operations: `success` (boolean) -- this is the
field `ValidateDescriptor` enforces for `CapabilityKubeconfig`, copying the
state pattern.

Contract notes:

- `exec_command` / `exec_args` are supplied by the host so the **host binary
  name** (embedder contract) is baked into the kubeconfig exec block; the
  provider never hardcodes `scafctl`. At login time the host bakes the resolved
  `--server`/`--audience` into static exec args.
- The provider is **stateless** -- it receives a `token` only for `whoami` and
  never caches it.
- `kubeconfig_path` empty means "resolve `KUBECONFIG` env or `~/.kube/config`"
  inside the plugin via `clientcmd` loading rules.
- `ca_data` is a PEM-encoded cluster CA bundle. When set, the written cluster
  entry pins the API server to this CA instead of falling back to
  `insecure_skip_tls`.
- `interactive_mode`, `install_hint`, and `provide_cluster_info` map to the
  kubeconfig exec block (`client.authentication.k8s.io`): `interactive_mode`
  (`Never`/`IfAvailable`/`Always`) controls whether kubectl may run the exec
  credential plugin interactively, `install_hint` is the message kubectl shows
  when the exec command is missing from `PATH`, and `provide_cluster_info` asks
  kubectl to pass cluster details to the plugin via `KUBERNETES_EXEC_INFO`.

## 5. Module / repo layout

### Host side (this repo)

- `pkg/provider` -- add `CapabilityKubeconfig` + `kubeconfigCapabilityRequiredFields`
  + `ValidateDescriptor`/`IsCapabilityValid` coverage (mirror `CapabilityState`).
- `pkg/kubeconfig/` (new) -- the manager, typed input/output structs, sentinels,
  and a `mock.go` provider for tests. Mirrors `pkg/state` file layout.
- `pkg/provider/official/official.go` -- add
  `{Name: "kubeconfig", CatalogRef: "kubeconfig", DefaultVersion: "latest",
  Description: "..."}` to `defaultProviders`. This makes it auto-fetchable via
  the existing `Fetcher` pipeline (`ToPluginDependency`).

### Plugin side (separate module / repo)

- New module mirroring the existing official providers
  (`git`, `exec`, ...), published to
  `ghcr.io/oakwood-commons/providers/kubeconfig`.
- Depends on `scafctl-plugin-sdk` v0.10.0 + `k8s.io/client-go`
  (`tools/clientcmd`, `rest`, `transport`) + `k8s.io/apimachinery`.
- Implements the SDK `Plugin` interface (`GetProviders`,
  `GetProviderDescriptor`, `ExecuteProvider`, `ExecuteProviderStream`,
  `DescribeWhatIf`, `ConfigureProvider`, `ExtractDependencies`, `StopProvider`);
  `main()` calls `sdkplugin.Serve(&KubeconfigPlugin{})`, modeled on
  `examples/plugins/echo/main.go`.
- `ExecuteProvider` switches on `input["operation"]` and routes to per-operation
  handlers.

### client-go boundary (verified)

Core already has `k8s.io/apimachinery` as a direct dep and `k8s.io/client-go`
only as indirect. Keeping the plugin in a separate module ensures the heavy
client-go packages (`clientcmd`/`rest`/`transport`) never enter core's direct
import graph. The shared typed structs live in `pkg/kubeconfig`
(apimachinery-free), and the plugin imports or mirrors them.

## 6. Interface Design

Define these contracts first (signatures illustrative; finalize during 2a):

~~~go
// pkg/provider/provider.go
const CapabilityKubeconfig Capability = "kubeconfig"

// pkg/kubeconfig/manager.go
type Manager struct {
    registry   *provider.Registry
    clients    []*plugin.Client // owned; killed on Close
    binaryName string
    // ... fetcher/official wiring resolved lazily from ctx
}

func NewManager(binaryName string, opts ...Option) *Manager

// ensure resolves "kubeconfig" from the registry, fetching+registering it on
// demand when absent (mirrors autoResolveProviderByName). Wraps fetch/execute
// failures as ErrProviderUnavailable for the Phase 3 fallback.
func (m *Manager) ensure(ctx context.Context) (provider.Provider, error)

func (m *Manager) WriteKubeconfig(ctx context.Context, in WriteInput) (WriteResult, error)
func (m *Manager) RemoveEntry(ctx context.Context, in RemoveInput) (RemoveResult, error)
func (m *Manager) CurrentServer(ctx context.Context, in CurrentServerInput) (string, error)
func (m *Manager) DetectAuthType(ctx context.Context, in DetectInput) (DetectResult, error)
func (m *Manager) Reachable(ctx context.Context, in ReachableInput) (ReachableResult, error)
func (m *Manager) Whoami(ctx context.Context, in WhoamiInput) (WhoamiResult, error)

func (m *Manager) Close() error // kills spawned plugin clients

// Sentinels
var (
    ErrProviderUnavailable = errors.New("kubeconfig: provider unavailable")
    ErrInvalidOperation    = errors.New("kubeconfig: invalid operation output")
)
~~~

Typed input/output structs (Huma validation tags, mirroring `kube.ClusterInfo`)
reuse `kube.AuthType` for `DetectResult.AuthType`. Each struct has a
`toInputs() map[string]any` and there is an `extract*` helper per result type
(direct `*Result` pointer first, then map fallback after JSON round-trip --
copying `state.extractStateData`).

## 7. Error Handling

- New sentinels: `ErrProviderUnavailable` (fetch/register/spawn failed -> Phase 3
  falls back to a static exec-credential kubeconfig) and `ErrInvalidOperation`
  (malformed `Output.Data`).
- Wrap with `fmt.Errorf("kubeconfig: <context>: %w", err)` throughout.
- Plugin side wraps clientcmd/rest errors and returns
  `Output{Data: {"success": false, ...}}` with a descriptive error so the host
  can surface a clear message.

## 8. Task Breakdown

### Phase 2a -- In-core contract + manager (this repo)

| # | Task | Files | Complexity | Depends on |
|---|------|-------|-----------|------------|
| 1 | Add `CapabilityKubeconfig` + required-output-fields + validation/IsCapabilityValid coverage | `pkg/provider/provider.go`, `pkg/provider/provider_test.go` | S | -- |
| 2 | Typed input/output structs (+ Huma tags), `toInputs`/`extract*` helpers, sentinels | `pkg/kubeconfig/types.go` | M | 1 |
| 3 | `Manager` with `ensure` (registry.Get -> fetch-then-register fallback), per-op methods, `Close()` | `pkg/kubeconfig/manager.go` | L | 1, 2 |
| 4 | `mock.go` provider implementing `CapabilityKubeconfig` + operation dispatch | `pkg/kubeconfig/mock.go` | M | 1, 2 |
| 5 | Table-driven manager tests against mock; embedder binary-name test; capability validation tests; benchmarks | `pkg/kubeconfig/*_test.go` | M | 3, 4 |
| 6 | Register `kubeconfig` in official providers; confirm fetcher cooldown/fallback path | `pkg/provider/official/official.go`, `official_test.go` | S | -- |

### Phase 2b -- External kubeconfig provider plugin (separate repo)

| # | Task | Complexity | Depends on |
|---|------|-----------|------------|
| 7 | New module: SDK `Plugin` impl, `kubeconfig` provider, `CapabilityKubeconfig`, descriptor + output schemas, `operation` dispatch, `main()`=`Serve` | M | 2 |
| 8 | `clientcmd` merge/write + remove + current-server (exec-credential user block) | L | 7 |
| 9 | `rest`-based `/healthz` reachability + `SelfSubjectReview` whoami | M | 7 |
| 10 | `detect_auth_type` (probe `.well-known/oauth-authorization-server` + OIDC discovery) -> oauth/oidc | M | 7 |
| 11 | Cluster-name sanitization helper in a small companion library (reused by Phase 4 OpenShift handler) | S | 8 |
| 12 | Plugin unit tests (temp `KUBECONFIG` + `httptest`), `mock.go`, benchmarks | M | 8, 9, 10 |
| 13 | CI/release: build, cosign-sign, publish to `ghcr.io/oakwood-commons/providers/kubeconfig` | M | 7-12 |

The bulk of host work (2a) lands and is fully testable against the mock before
the plugin exists.

## 9. Testing Strategy

- **In-core (2a):** table-driven `Manager` tests against `mock.go` (no client-go
  in core test deps); capability-validation tests for `ValidateDescriptor`;
  an embedder test asserting a non-default binary name (e.g. `"mycli"`) flows
  into `exec_command`; `extract*` round-trip tests (pointer + map paths);
  benchmarks for the hot `toInputs`/`extract` path. Target 70%+ patch coverage.
- **Plugin (2b):** `clientcmd` write/remove/current-server against a temp
  `KUBECONFIG` file; `httptest` servers for `reachable` (`/healthz`),
  `detect_auth_type` (well-known endpoints), and `whoami` (`SelfSubjectReview`);
  `mock.go` for the ops interface; per-provider benchmarks (provider
  conventions). Negative cases: unreachable server, malformed kubeconfig,
  ambiguous detection.
- **Integration:** deferred to Phase 3, where the CLI command wires the manager
  end-to-end against a fake API server (CLI-scope test under
  `tests/integration/`).
- **E2E:** run `task test:e2e` once after 2a, redirect to a file, grep results.

## 10. Embedder / binary-name handling

- `pkg/kubeconfig.Manager` is constructed with the host binary name
  (`settings.Run.BinaryName`, falling back to `settings.CliBinaryName`) and
  passes it as `exec_command` so embedders (e.g. `cldctl`) get correct kubeconfig
  exec args. No hardcoded `scafctl`.
- The fetch-then-register path uses the manager's configured binary name
  (`m.binaryName`) for the `plugin.ProviderConfig.BinaryName`, keeping plugin
  cache paths/config consistent with the `exec_command` baked into the
  kubeconfig and self-contained for embedders calling the package directly.
- A manager test must assert the baked `exec_command`/`exec_args` for a non-
  default binary name.

## 11. Documentation & Examples

- Update `docs/design/kubernetes-auth.md` to mark Phase 2 design-complete and
  link this plan.
- Add an `examples/` kubeconfig snippet only once Phase 3 wires the command
  (the provider is not directly user-invokable in a solution).
- MCP: no new tool in Phase 2 (no user-facing command yet).
- Markdown: ASCII-only, tilde fences for blocks containing backticks, no emojis,
  `--` not em dashes.

## 12. Risks & Sub-decisions

- **Plugin lifecycle for one-shot commands (the main difference from state).**
  The manager spawns plugin clients and must `Kill()` them on `Close()`; the
  Phase 3 command `defer`s it. Reuse one client across all ops in a single
  invocation. Resolved approach: model on `autoResolveProviderByName` +
  manager-owned `Close()`.
- **Auto-fetch outside a solution -- RESOLVED.** No pool pass populates the
  registry for a command-initiated call; the manager does its own fetch-then-
  register via `official.RegistryFromContext` + `prepare.BuildPluginFetcher` +
  `FetchPlugins` + `plugin.RegisterFetchedPlugins`. Failure -> `ErrProviderUnavailable`
  -> Phase 3 static exec-credential fallback.
- **`CapabilityKubeconfig` vs `CapabilityAction` -- RESOLVED** in favor of a
  dedicated capability (clean validation/discovery, mirrors state).
- **Detection reliability.** `detect_auth_type` is best-effort; an embedder-set
  `ClusterInfo.AuthType` always wins over probing.
- **Companion library boundary.** Keep sanitization/detection helpers in a small
  library within the plugin module so the Phase 4 OpenShift handler can import
  them; finalize the boundary in task 11.
- **Security.** TLS verification on by default; `insecure_skip_tls` is opt-in and
  documented as development-only. The provider never logs the `whoami` token.

## 13. Out of Scope (later phases)

- Phase 3: `auth login <handler> --cluster` / `auth logout --cluster` wiring +
  fallback policy.
- Phase 4: OpenShift OAuth handler plugin.
- Token caching: reuses the existing keyring `TokenCache` (Q4); the provider
  never touches it.

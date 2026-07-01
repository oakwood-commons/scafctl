---
title: "Host-Aware Login and Hostname Resolution"
weight: 24
---

# Host-Aware Login and Hostname Resolution

**Issues**:
[#569](https://github.com/oakwood-commons/scafctl/issues/569),
[#570](https://github.com/oakwood-commons/scafctl/issues/570),
[#571](https://github.com/oakwood-commons/scafctl/issues/571),
[#572](https://github.com/oakwood-commons/scafctl/issues/572)
**Status**: Implemented (not yet released)

---

## Problem

Some auth handlers (notably an OpenShift/Kubernetes-style handler) authenticate
against a specific endpoint. Users must pass the concrete API server URL at
login:

~~~bash
scafctl auth login openshift --hostname https://api.cluster-01.example.com:6443
~~~

For a large fleet (hundreds of clusters) this is error-prone and unmemorable.
Users want to log in with a short selector:

~~~bash
scafctl auth login openshift --hostname cluster-01
~~~

The mapping from selector to endpoint must be:

- **Configurable per handler** -- different handlers have different fleets.
- **Static or dynamic** -- small teams alias a handful of endpoints by hand;
  large orgs have a live inventory endpoint listing every cluster.
- **Shape-blind in the core** -- scafctl must not hardcode any organization's
  inventory JSON format. Each org adapts its own shape.
- **Non-interactive at resolution time** -- resolving a selector must never
  trigger a browser login on its own.

Two supporting configuration capabilities were needed to make this ergonomic:

- A CLI to manage static aliases without hand-editing YAML (#569).
- A drop-in `config.d` layering mechanism so tooling can supply handler and
  hostname config without clobbering the user's main file (#571), plus a way
  for embedders to discover the config directory (#572).

---

## Design

### Configuration model (#570)

Per-handler configuration lives under `auth.handlers.<name>`. The host consumes
a reserved `hostname` block; every other key is forwarded opaquely to the
handler plugin.

~~~yaml
auth:
  handlers:
    openshift:
      hostname:
        aliases:
          prod: https://api.prod.example.com:6443
        resolver:
          source:
            url: https://clusters.example.com/inventory
            authProvider: entra
            authScope: api://fleet-inventory/.default
            headers:
              Accept: application/json
          transform: |
            _.map(k, _[k].status != "deleted", {
              "name": k,
              "url": _[k].apiServerURL,
            })
          ttl: 1h
      # Any other key (not "hostname") is passed to the plugin unchanged.
      apiTimeout: 30s
~~~

Types (in `pkg/config/types.go`):

- `GlobalAuthConfig.Handlers map[string]HandlerConfig`
- `HandlerConfig{ Hostname *HostnameConfig; Settings map[string]any }`
  -- `Settings` uses `mapstructure:",remain"` to capture passthrough keys.
- `HostnameConfig{ Aliases map[string]string; Resolver *HostnameResolverConfig }`
- `HostnameResolverConfig{ Source HostnameResolverSource; Transform string; TTL string }`
- `HostnameResolverSource{ URL, AuthProvider, AuthScope string; Headers map[string]string }`

The core is shape-blind: an organization adapts its inventory response entirely
through the CEL `transform`. The canonical normalized contract is a
**list of `{name, url}` objects**.

### Resolution precedence

For a given `--hostname <selector>`, the host resolves in this order (first
match wins):

1. **Concrete URL** -- the selector already parses as an `http`/`https` URL;
   forward unchanged.
2. **Static alias** -- a `hostname.aliases` entry.
3. **Dynamic resolver** -- fetch the inventory, run the transform, match by
   `name`.
4. Otherwise fail, listing the available selectors.

Static aliases overlay the dynamic inventory: if both define the same selector,
the alias wins.

### Host resolution package (`pkg/auth/hostname`)

The resolver is a small, dependency-injected core so it is fully unit-testable
without network or disk:

~~~go
func ResolveWith(
    ctx context.Context,
    cfg *config.HostnameConfig,
    handler, selector string,
    deps Deps,
) (string, error)
~~~

`Deps` carries `Fetch`, `Token`, `Transform`, `Cache`, and `Now`, each with a
production default via `withDefaults()`. `Resolve(ctx, handler, selector)` is
the thin production entry point: it pulls the handler's `HostnameConfig` from
`config.FromContext(ctx)` and calls `ResolveWith` with disk-backed defaults.

Sentinel errors (`errors.go`): `ErrSelectorNotFound`, `ErrResolverLoop`,
`ErrTransformShape`, `ErrNoCredentials`.

- **Fetch** (`fetch.go`) -- validates the URL scheme, builds an `httpc` client
  from the app HTTP config, GETs with `Accept: application/json`, static
  headers, and an optional `Bearer` token, and reads a size-limited body.
- **Transform** (`transform.go`) -- JSON-unmarshals the body, evaluates the CEL
  transform (`celexp.EvaluateExpression`) with the body bound to `_`, then
  round-trips the result through JSON into `[]Entry`, validating that each entry
  has a non-empty `name` and `url`. Any other shape yields `ErrTransformShape`.
- **Token** (`token.go`) -- `tokenprovider.GetToken` with
  `Caller: callerscope.ServerCaller`. This is cached and non-interactive: if the
  user is not already logged in to the `authProvider`, it errors (wrapped as
  `ErrNoCredentials` with a hint to run `auth login <provider>` first) rather
  than opening a browser.

**Loop guard**: a resolver whose `source.authProvider` equals the handler being
logged into is rejected (`ErrResolverLoop`) -- a handler cannot bootstrap its
own inventory token.

### On-disk TTL cache

A single-shot CLI process cannot benefit from an in-memory cache, so the
inventory cache is a dedicated on-disk JSON store under
`paths.CacheDir()/hostname/`.

- Key: `sha256(handler \0 source.URL \0 transform)`.
- File: `{ ExpiresAt time.Time, Entries []Entry }`, written `0600` in a `0700`
  directory.
- `Get` treats expired or corrupt entries as a miss (and removes expired ones);
  `Set` is skipped when `ttl <= 0`.
- `ttl` parsing treats `""`, `"0"`, negative, and unparseable values as "no
  caching".

### Login hook (`pkg/cmd/scafctl/auth/login.go`)

After the existing `--hostname`/`CapHostname` capability gate, the command
resolves the selector before delegating to the handler:

~~~go
if hostname != "" {
    resolved, err := hostnameresolver.Resolve(ctx, handlerName, hostname)
    // ErrSelectorNotFound / ErrResolverLoop / ErrTransformShape -> InvalidInput
    // otherwise -> GeneralError
    hostname = resolved
}
~~~

The resolved concrete URL is what the plugin receives. Resolution failures never
call the handler.

### `auth alias` command (#569)

A thin CLI over the static `aliases` map, written to the user's main
`config.yaml`:

~~~bash
scafctl auth alias set openshift prod https://api.prod.example.com:6443
scafctl auth alias list openshift          # kvx table / -o json / -o yaml
scafctl auth alias remove openshift prod   # prunes empty hostname/handler entries
~~~

All subcommands gate on `auth.CapHostname` via `getHandler` + `HasCapability`.
The command follows scafctl conventions: `writer.FromContext` for status
output, `kvx` for structured `list` output, and `settings.CliBinaryName` ->
`cliParams.BinaryName` substitution so embedders get their own binary name in
help text.

### Config layering: `config.d` (#571) and `ConfigDir` (#572)

`Manager.Load()` merges `*.yaml`/`*.yml` fragments from a `config.d/` directory
next to the main config file, in lexical filename order, **between** the
embedder base layer and the user's `config.yaml`.

Precedence (lowest to highest):

1. Built-in defaults
2. Embedder base config (`WithBaseConfig`)
3. `config.d/*.yaml` fragments (lexical order)
4. `config.yaml` (user's main file)
5. Environment variables (`SCAFCTL_*`)
6. Command-line flags

Maps deep-merge across layers; arrays (e.g. `catalogs`) are replaced by the
highest-precedence layer that defines them. A missing `config.d` is not an
error; a malformed fragment fails the load with the offending filename.

`Manager.ConfigDir()` returns the directory containing the config file -- the
anchor for `config.d` and for embedders that need to locate config-relative
resources.

---

## Key decisions

1. **Cache backend: dedicated on-disk JSON, not in-memory LRU.** A single-shot
   CLI invocation has no long-lived process, so an in-memory TTL cache never
   hits. The on-disk store honors `resolver.ttl` across invocations.
2. **Token caller scope: reuse `callerscope.ServerCaller`.** Matches the
   existing non-interactive enumerator precedent -- resolution reads cached
   tokens only and never triggers interactive login.
3. **`auth alias set` writes to the main `config.yaml`.** Aliases are
   user-owned state; they belong in the user's primary file, not a `config.d`
   fragment (which is for tooling-supplied layers).
4. **#572 scope is minimal.** `ConfigDir()` plus `WithBaseConfig`/`config.d`
   cover the need; no new merge path was introduced.

### Serialization note

`HandlerConfig.Settings` is `mapstructure:",remain"` for decode and
`yaml:",inline"` for encode. The inline tag is required so opaque passthrough
settings survive a save round-trip (e.g. when `auth alias` rewrites
`config.yaml`); without it viper's `WriteConfig` would drop them. Viper
lowercases all config keys, so plugins receive lowercased setting names.

---

## Security considerations

- **No interactive login during resolution.** Inventory token acquisition is
  cached-only; unauthenticated providers fail with a clear hint.
- **Trusted-config fetch.** The inventory URL comes from admin-owned config; the
  fetch validates the scheme (`http`/`https`) and bounds the response size.
- **Loop protection.** A resolver cannot use the handler it configures as its
  own `authProvider`.
- **Cache isolation.** Cache files are keyed by handler + URL + transform and
  written with restrictive permissions (`0600` in a `0700` directory).

---

## Testing

- `pkg/auth/hostname` -- precedence matrix, loop guard, no-credentials path,
  transform-shape errors, token injection, cache hit/miss/expiry, TTL parsing,
  URL validation. Includes the map-keyed transform
  (`_.map(k, {"name": k, "url": _[k].apiServerURL})`).
- `pkg/cmd/scafctl/auth` -- login hook (alias resolved, selector-not-found,
  concrete-URL passthrough, embedder binary name) and the `auth alias` command
  (set/list/remove, capability rejection, prune, opaque-settings round-trip,
  embedder binary name).
- `pkg/config` -- `config.d` merge order and precedence, missing/ malformed
  fragments, and `ConfigDir` for explicit and default paths.

---

## Related

- [Authentication](auth.md)
- [Multi-Profile Authentication](multi-profile-auth.md)
- [Identity Provider](identity-provider.md)
- Examples: `examples/auth/hostname-resolution.md`,
  `examples/auth/README.md`, `examples/config/README.md`

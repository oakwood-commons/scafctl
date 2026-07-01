# Host-Aware Login: Hostname Aliases and Dynamic Resolution

Some auth handlers advertise the `hostname` capability, meaning they accept a
`--hostname <selector>` argument at login. The scafctl host resolves that
selector into a concrete endpoint URL **before** delegating to the handler
plugin. This lets users log in with a short, memorable selector (e.g. `prod`)
instead of a long API server URL.

Resolution is entirely host-side and shape-blind: the core never hardcodes any
organization's inventory format. Organizations adapt their inventory shape with
a single CEL transform.

## Resolution precedence

For a given `--hostname <selector>`, the host resolves in this order (first
match wins):

1. **Concrete URL** -- if the selector already parses as an `http`/`https` URL,
   it is forwarded unchanged.
2. **Static alias** -- a `hostname.aliases` entry mapping selector to URL.
3. **Dynamic resolver** -- `hostname.resolver` fetches an inventory and
   normalizes it into `{name, url}` entries; the selector is matched by `name`.
4. Otherwise, login fails and lists the available selectors.

## Static aliases

Manage aliases with the `auth alias` command (they are written to your main
`config.yaml`):

```bash
scafctl auth alias set openshift prod https://api.prod.example.com:6443
scafctl auth alias set openshift stg  https://api.stg.example.com:6443
scafctl auth alias list openshift
scafctl auth login openshift --hostname prod
```

Equivalent config:

```yaml
auth:
  handlers:
    openshift:
      hostname:
        aliases:
          prod: https://api.prod.example.com:6443
          stg: https://api.stg.example.com:6443
```

## Dynamic resolution

When a fleet has too many endpoints to alias by hand, configure a resolver. At
login time the host performs an HTTPS GET, runs a CEL transform to normalize the
response into a list of `{name, url}` entries, and caches the result for the
configured TTL.

```yaml
auth:
  handlers:
    openshift:
      hostname:
        # Static aliases still win over the resolver when both match.
        aliases:
          sandbox: https://api.sandbox.example.com:6443
        resolver:
          source:
            url: https://clusters.example.com/inventory
            # Optional: inject a bearer token from another auth handler.
            authProvider: entra
            authScope: api://fleet-inventory/.default
            headers:
              Accept: application/json
          # CEL transform: normalize the fetched JSON (bound to `_`) into a
          # list of {name, url} objects. Filter out anything not usable.
          transform: |
            _.map(k, _[k].status != "deleted", {
              "name": k,
              "url": _[k].apiServerURL,
            })
          # Cache the resolved inventory for this long. "0" or empty disables
          # caching (re-fetch on every login).
          ttl: 1h
```

### The transform contract

- The fetched response body is parsed as JSON and bound to `_`.
- The transform must produce a **list of objects**, each with a non-empty
  `name` (string) and `url` (string).
- Any other shape fails with a clear "transform shape" error.

Each entry may also carry optional per-cluster OIDC metadata. These fields
mirror the cluster model used by `kube login`, so a single inventory can drive
both `auth login --hostname` and `kube login`:

| Field | Type | Meaning |
| --- | --- | --- |
| `audience` | string | OIDC audience / client ID a token for this cluster targets |
| `authType` | string | Login method: `""` (auto), `oauth`, or `oidc` |
| `caData` | string | PEM-encoded CA bundle for the endpoint |
| `consoleUrl` | string | Optional web console URL |
| `insecureSkipTls` | bool | Disable endpoint TLS verification (dev only) |

Omitted OIDC fields fall back to auto-detection at login time.

The example above handles a **name-keyed map** response, e.g.:

```json
{
  "cluster-01": { "clusterName": "cluster-01", "apiServerURL": "https://api.cluster-01.example.com:6443", "clientID": "api://cluster-01/.default", "status": "active" },
  "cluster-02": { "clusterName": "cluster-02", "apiServerURL": "https://api.cluster-02.example.com:6443", "clientID": "api://cluster-02/.default", "status": "deleted" }
}
```

`_.map(k, cond, expr)` iterates the map keys `k`, keeps those where `cond` is
true, and emits `expr`. To surface OIDC metadata, add the optional fields to the
emitted object:

```yaml
transform: |
  _.map(k, _[k].status != "deleted", {
    "name": k,
    "url": _[k].apiServerURL,
    "audience": _[k].clientID,
    "authType": "oidc"
  })
```

If instead your endpoint returns a **JSON array**, use a list comprehension:

```yaml
transform: |
  _.filter(c, c.state == "ready").map(c, {"name": c.id, "url": c.endpoint})
```

## Caching

Resolved inventories are cached on disk under the scafctl cache directory
(`hostname/`), keyed by a hash of the handler name, source URL, and transform.
The cache honors the resolver `ttl`; expired or corrupt entries are ignored and
re-fetched. Set `ttl: "0"` (or omit it) to disable caching entirely.

## Authenticated inventory endpoints

When `source.authProvider` is set, the host requests a **cached, non-interactive**
token from that handler and injects it as a `Bearer` header. If you are not
already logged in to that provider, resolution fails with a hint to run
`auth login <provider>` first -- the inventory fetch never triggers an
interactive browser login on its own.

To avoid a resolver depending on the very handler it configures (an infinite
loop), the host rejects a resolver whose `authProvider` equals the handler being
logged into.

## Troubleshooting

- **`selector not found`** -- run `auth alias list <handler>` to see static
  aliases, or check the resolver `source.url` and `transform`. The error lists
  the selectors the resolver produced.
- **`transform shape`** -- the CEL transform did not return a list of
  `{name, url}` objects. Validate it against a sample of the endpoint response.
- **`no credentials`** -- the resolver's `authProvider` is not logged in. Run
  `auth login <authProvider>` first.

## Related

- [Auth examples README](README.md) -- `auth alias` quick reference
- [Auth Tutorial](../../docs/tutorials/auth-tutorial.md)

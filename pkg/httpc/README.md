# pkg/httpc -- HTTP Client Adapter

Thin adapter over [`github.com/oakwood-commons/httpc`](https://github.com/oakwood-commons/httpc) that adds scafctl-specific behaviour:

- **XDG cache directory** via `pkg/paths` instead of `os.UserCacheDir`
- **App-name-derived cache key prefix** via `pkg/settings` (for example, `scafctl:` for the default binary name)
- **OTel metrics bridge** -- `OTelMetrics` implements the upstream `Metrics` interface using `pkg/metrics` OTel instruments
- **Dial-time SSRF policy** -- `PolicyFromAppConfig`/`PolicyFromContext` build an `IPPolicy` enforced by the transport when each connection is dialed
- **`config.HTTPClientConfig` bridge** -- `NewClientFromAppConfig` converts the string-based app config to a typed `ClientConfig`

## Usage

All consumers import this package as before:

~~~go
import "github.com/oakwood-commons/scafctl/pkg/httpc"

client := httpc.NewClient(nil)            // scafctl defaults
client := httpc.NewClient(&httpc.ClientConfig{...})
client := httpc.NewClientFromAppConfig(cfg, logger)
config := httpc.DefaultConfig()
~~~

## Re-exported symbols

Types, constants, errors, and functions from the upstream library are re-exported
via type aliases and `var` assignments. Consumers should not need to import
`github.com/oakwood-commons/httpc` directly.

## SSRF protection

SSRF enforcement happens at **dial time** in the upstream library (httpc v0.3.0+),
via an `IPPolicy` attached to the client's transport. This closes the
TOCTOU/DNS-rebinding gap an earlier, preflight-only design had: the address
actually dialed is checked, not just the address a URL initially resolved to.

- `PolicyFromAppConfig(cfg *config.HTTPClientConfig) (*upstream.IPPolicy, error)`
  builds the policy from application configuration (`httpClient.allowPrivateIPs`,
  `httpClient.allowedPrivateCIDRs`, `httpClient.trustProxyResolution`).
- `PolicyFromContext(ctx)` derives the same policy from `config.FromContext(ctx)`,
  for callers that only have a context.
- The zero-value policy denies private, loopback, and link-local addresses; cloud
  metadata addresses are denied unconditionally and cannot be re-enabled by any
  configuration.
- `ExplainBlocked(err)` rewrites a policy denial to name the configuration key
  that would permit it, instead of the upstream library's internal field name.

The `PrivateIPsAllowed(ctx)` preflight helper is gone -- callers no longer
enforce SSRF themselves at the call site; the policy above is applied once,
on the transport, and enforced on every dial. (`ValidateURLNotPrivate(url)`
is still re-exported for ad-hoc, call-site text checks, but policy-protected
call sites in this repo no longer rely on it -- the dial-time check
supersedes it.)

### Proxy routing is disabled unless explicitly trusted

A proxied request is dialed to the proxy, not the target, so the dial-time
check above never runs for that hop -- the target is instead checked once
against a local DNS answer before the request is handed to the proxy. A
proxy whose own resolution differs (split-horizon DNS, a rebind between
check and hand-off) can still connect somewhere that local check never saw.

- `ProxyAwareTransport(trustedProxy bool) http.RoundTripper` returns a
  transport with proxy selection disabled (`trustedProxy=false`, the
  default) or `nil` (`trustedProxy=true`, leaving `http.DefaultTransport`'s
  normal `HTTP_PROXY`/`HTTPS_PROXY` behavior in place).
- `TrustedProxy(cfg)` / `TrustedProxyFromContext(ctx)` read
  `httpClient.trustedProxy` from application configuration; both default to
  `false`.
- `NewClientFromAppConfig` and every other policy-protected client
  constructor in this package wire `ProxyAwareTransport` in automatically
  when the caller has not already supplied its own `Transport`. Set
  `httpClient.trustedProxy: true` only once the configured proxy is known
  to enforce an equivalent destination-address policy itself.

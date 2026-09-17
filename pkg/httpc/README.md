# pkg/httpc -- HTTP Client Adapter

Thin adapter over [`github.com/oakwood-commons/httpc`](https://github.com/oakwood-commons/httpc) that adds scafctl-specific behaviour:

- **XDG cache directory** via `pkg/paths` instead of `os.UserCacheDir`
- **App-name-derived cache key prefix** via `pkg/settings` (for example, `scafctl:` for the default binary name)
- **OTel metrics bridge** -- `OTelMetrics` implements the upstream `Metrics` interface using `pkg/metrics` OTel instruments
- **Context-based SSRF checks** -- `PrivateIPsAllowed(ctx)` reads `config.FromContext(ctx)` to decide whether private IPs are allowed
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

The removed `PrivateIPsAllowed(ctx)` / `ValidateURLNotPrivate(url)` preflight
helpers are gone -- callers no longer enforce SSRF themselves at the call site;
the policy above is applied once, on the transport, and enforced on every dial.

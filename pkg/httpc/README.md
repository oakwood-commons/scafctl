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

The upstream library has transport-level SSRF protection (`AllowPrivateIPs` field
on `ClientConfig`). This adapter disables it (`AllowPrivateIPs = true` on every client)
because scafctl performs SSRF validation at the application layer via
`PrivateIPsAllowed(ctx)` and `ValidateURLNotPrivate(url)` in the providers that
need it (httpprovider, parameterprovider).

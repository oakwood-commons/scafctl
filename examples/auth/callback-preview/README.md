# OAuth Callback Page Preview Gallery

Interactive local harness that runs all **8 browser OAuth callback completion
scenarios** end-to-end against real loopback `oauth.StartCallbackServer`
instances with deferred completion (`WithDeferredCompletion()`), production
`Content-Security-Policy` headers, and configurable post-callback token exchange
delay.

## Quick Start

Launch from the repository root with Task:

~~~bash
task preview:authpage
~~~

Or run directly with Go flags:

~~~bash
go run ./examples/auth/callback-preview -port 8787 -hold 1500ms
~~~

### Flags

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `-port` | `8787` | Loopback port for the gallery (`0` selects an ephemeral port) |
| `-hold` | `1500ms` | Simulated post-callback token exchange and keyring save delay |
| `-open` | `true` | Open `http://localhost:<port>` in your default browser on startup |

You can also switch the simulated exchange delay (`0s`, `1.5s`, `4s`) directly
from the toolbar at the top of the gallery page without restarting the server.

## Scenarios Covered

1. **`scafctl` Blueprint (Success)** -- HTTP `200 OK`, default `scafctl`
   Blueprint layout with embedded Configuration Flow SVG lockup and favicon.
2. **`scafctl` Blueprint (Failure)** -- HTTP `503 Service Unavailable`, amber
   Blueprint failure layout when post-callback token exchange or storage fails.
3. **Alternate Binary Brand (Success)** -- HTTP `200 OK`, embedder sets
   `BinaryName: "acme-cli"` with `AuthPageRenderer: nil`, rendering a plain-text
   wordmark with zero `scafctl` artwork.
4. **Alternate Binary Brand (Failure)** -- HTTP `503 Service Unavailable`,
   amber Blueprint failure layout with the `"acme-cli"` plain-text wordmark.
5. **Custom `authpage.Renderer` (Success)** -- HTTP `200 OK`, complete
   whole-document replacement via `RootOptions.AuthPageRenderer`.
6. **Custom `authpage.Renderer` (Failure)** -- HTTP `503 Service Unavailable`,
   custom whole-document failure page served with a non-2xx status code.
7. **`oauth-helpers` Dark `#05152a` Fallback (Success)** -- HTTP `200 OK`,
   `StartCallbackServer` with `WithDeferredCompletion()` and no `CallbackPages`
   bundle, displaying `oauth-helpers`'s built-in brand-neutral dark card.
8. **`oauth-helpers` Dark `#05152a` Fallback (Failure)** -- HTTP `503 Service Unavailable`,
   `oauth-helpers`'s built-in dark `#05152a` failure card with zero leaked
   provider details.

## Related

- [Customizing the Browser OAuth Callback Page](../custom-auth-page.md)
- [Embedder Guide: Custom Browser Authentication Pages](../../../docs/design/auth-callback-pages.md)

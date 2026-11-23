# Customizing the Browser OAuth Callback Page (Embedder API)

When embedding `scafctl` as a library CLI (for example `mycli`), you can
customize or completely replace the HTML page shown in the user's browser after
an interactive OAuth redirect.

Customization is intentionally **Go-only** (not a CLI flag or user config path)
so untrusted configuration files cannot inject arbitrary HTML into localhost
authentication flows.

## Default Behavior for Alternate Binary Names

Setting `RootOptions.BinaryName` alone automatically switches the default
Blueprint header from the embedded `scafctl` SVG lockup to a plain-text
wordmark using your binary name:

~~~go
cmd, cleanup := scafctl.Root(&scafctl.RootOptions{
    BinaryName: "mycli",
})
defer cleanup()
~~~

## Complete Whole-Document Replacement

Supply `RootOptions.AuthPageRenderer` to return your own self-contained HTML
documents for `authpage.StateSuccess` and `authpage.StateFailure`. Keep styles
inline and embed any images as `data:` URIs -- external network requests and
scripts are blocked by the callback server's `Content-Security-Policy`.

~~~go
package main

import (
    "context"
    "fmt"
    "html"
    "os"

    "github.com/oakwood-commons/scafctl/pkg/authpage"
    "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl"
)

func main() {
    cmd, cleanup := scafctl.Root(&scafctl.RootOptions{
        BinaryName: "mycli",
        AuthPageRenderer: func(_ context.Context, d authpage.Data) ([]byte, error) {
            brand := html.EscapeString(d.BinaryName)
            if d.State == authpage.StateSuccess {
                return []byte(fmt.Sprintf(
                    "<!doctype html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body><h1>Connected to %s</h1><p>You may close this tab.</p></body></html>",
                    brand, brand,
                )), nil
            }
            return []byte(fmt.Sprintf(
                "<!doctype html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body><h1>Sign-in failed</h1><p>Return to your terminal for details.</p></body></html>",
                brand,
            )), nil
        },
    })
    defer cleanup()

    if err := cmd.Execute(); err != nil {
        os.Exit(1)
    }
}
~~~

See [Embedder Guide: Custom Browser Authentication Pages](../../docs/design/auth-callback-pages.md)
for the deferred completion lifecycle, security headers, and `pkg/auth/oauth2`
direct domain options, or run `task preview:authpage` (see
[callback-preview/README.md](callback-preview/README.md)) to preview all 8
built-in, embedder, and fallback callback pages locally in your browser.

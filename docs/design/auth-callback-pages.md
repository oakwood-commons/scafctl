---
title: "Embedder Guide: Custom Browser Authentication Pages"
---

# Embedder Guide: Custom Browser Authentication Pages

Interactive OAuth 2.0 logins (`auth login` and `kube login` using the
authorization-code or implicit-grant flows) start a temporary loopback HTTP
server on `localhost` to receive the identity provider redirect.

By default:

- **`scafctl`** renders the self-contained **Blueprint** completion page with
  the embedded Configuration Flow logo and favicon.
- **Alternate binary names** (`RootOptions.BinaryName: "mycli"` with
  `AuthPageRenderer: nil`) render the Blueprint layout with an HTML-escaped
  plain-text `mycli` wordmark instead of `scafctl` artwork.
- **Custom embedder renderers** (`RootOptions.AuthPageRenderer` or
  `oauth2.WithAuthPageRenderer`) replace the **entire HTML document** for both
  success and failure outcomes.

## Deferred Completion Lifecycle

The browser page is held pending until **all fatal post-callback steps**
complete:

1. The browser lands on `http://localhost:<port>` with `?code=...&state=...`.
2. The callback server validates `state` and delivers the code immediately,
   keeping the HTTP response open.
3. Token exchange, credential storage, and identity claims extraction run.
4. Only after those steps succeed does the server write `200 OK` with the
   success HTML document (or `503 Service Unavailable` with the safe failure
   HTML document if any fatal step failed).

If the user completes login via remote **paste-back** without a browser
reaching `localhost`, the callback server closes immediately without waiting.

## Security Model

- **Zero sensitive inputs**: `authpage.Renderer` receives only
  `authpage.Data{BinaryName, State}`. Tokens, authorization codes, OAuth state,
  redirect URLs, identity claims, and raw provider error strings never enter
  the renderer or the browser DOM.
- **Strict headers**: `oauth-helpers` serves final documents with
  `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'`,
  `Referrer-Policy: no-referrer`, `Cache-Control: no-store`, and
  `X-Content-Type-Options: nosniff`.
- **Safe fallbacks**: If a custom renderer returns an error, empty output, or a
  document larger than `authpage.MaxDocumentBytes` (256 KiB), `authpage.RenderSafe`
  substitutes a minimal dependency-free HTML fallback matching the true
  `StateSuccess` / `StateFailure` outcome. Presentation errors never turn a
  successful login into a failure or vice versa.

## Replacing the Entire Document via `RootOptions`

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

func renderAcmeAuthPage(_ context.Context, d authpage.Data) ([]byte, error) {
    brand := html.EscapeString(d.BinaryName)
    title := "Signed in"
    body := "Authentication succeeded. Return to your terminal."
    if d.State == authpage.StateFailure {
        title = "Sign-in failed"
        body = "Check your terminal output for details and try again."
    }
    doc := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>%s - %s</title>
  <style>
    body { font-family: system-ui, sans-serif; margin: 4rem auto; max-width: 36rem; padding: 0 1.5rem; }
  </style>
</head>
<body>
  <h1>%s</h1>
  <p>%s</p>
</body>
</html>`, brand, title, title, body)
    return []byte(doc), nil
}

func main() {
    cmd, cleanup := scafctl.Root(&scafctl.RootOptions{
        BinaryName:       "mycli",
        AuthPageRenderer: renderAcmeAuthPage,
    })
    defer cleanup()
    if err := cmd.Execute(); err != nil {
        os.Exit(1)
    }
}
~~~

## Direct Domain Usage (`pkg/auth/oauth2`)

Callers constructing `pkg/auth/oauth2.Handler` directly without `Root()` pass
the same renderer and binary name via functional options:

~~~go
handler, err := oauth2.New(
    cfg,
    oauth2.WithBinaryName("mycli"),
    oauth2.WithAuthPageRenderer(renderAcmeAuthPage),
)
~~~

## Plugin Transport

When `Root()` configures auth handler plugins (eager `RootOptions.AuthPluginDirs`
loading, on-demand catalog fallback resolution, cached lazy handlers, and
runtime CLI flag overrides such as `--tenant` or `--client-id`), it pre-renders
the success and failure documents lazily on first plugin start and attaches them
to `ConfigureAuthHandlerRequest.auth_callback_pages`. Upgraded plugins (such as
`scafctl-plugin-auth-entra`) pass those bytes straight to `oauth-helpers` with
deferred completion enabled; older plugins ignore the additive protobuf field.

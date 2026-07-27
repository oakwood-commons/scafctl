---
title: "Embedder Guide: Registering Go-Template Functions"
---

# Embedder Guide: Registering Go-Template Functions

This document explains how embedders (e.g., cldctl) add custom Go-template
functions that become available to every template scafctl renders -- resolver
templates, the `go-template` provider, `evaluate`, and functional tests.

## Scope: Go-only, not YAML

Template functions are compiled Go code. An embedder registers them from its
own `main` before handing control to scafctl. Individual **solution YAML files
cannot declare functions** -- there is deliberately no YAML or provider-input
surface for arbitrary code, which would be a code-injection footgun. Solutions
consume whatever the built binary exposes.

## The embedder API surface

There are two entry points, in increasing order of power:

- **`RootOptions.GoTemplateFuncs template.FuncMap`** -- the ergonomic path. The
  root command registers these during `PersistentPreRun`, after the extension
  factory is initialized, so built-in collisions are detectable.
- **`gotmpl.RegisterFuncs` / `gotmpl.RegisterFuncsOverride`** -- the underlying
  package API, callable directly if the embedder wires templates without
  `Root()`.

~~~go
cmd, cleanup := scafctl.Root(&scafctl.RootOptions{
    BinaryName: "mycli",
    GoTemplateFuncs: template.FuncMap{
        "shout": func(s string) string { return strings.ToUpper(s) + "!" },
    },
})
defer cleanup()
~~~

## Registration model

Registration is **package-global and additive**. Functions are merged at the
single choke point (`getExtensionFuncMap`) that every `gotmpl` service uses, so
all render paths pick them up automatically. The extension factory itself
remains `sync.Once`-guarded; the registry is a layer on top.

`RegisterFuncs` is **all-or-nothing**: if any name in the batch collides with a
built-in (sprig or custom scafctl function) or a previously registered name, the
entire call is rejected with `ErrFuncNameCollision` and nothing is registered.
This refusal to silently shadow a built-in means a typo cannot quietly change
template behavior.

Registration is **register-once**, matching the Go standard library's global
registries (for example `sql.Register`): re-registering an already-present name
-- even with the identical function -- is a collision. Register a given set of
functions a single time. The root command guards its own wiring (see below), so
executing the command more than once in a long-running host does not
re-register or self-collide. Values are also validated up front (a nil value, a
non-function, or a function whose return signature `text/template` rejects fails
fast rather than panicking later at template construction).

`RegisterFuncsOverride` is the escape hatch: it unconditionally replaces an
existing function (including a built-in). Use it only when shadowing is
intentional.

### Precedence when a template renders

Later entries win:

1. Built-in factory functions (sprig + custom scafctl functions)
2. Environment stripping -- unless environment access is enabled, `env` and
   `expandenv` are removed at this layer so secrets are not exposed by default
3. Additive embedder functions (`RegisterFuncs`) -- applied only for names not
   already provided by a built-in, and never re-introducing a stripped
   `env`/`expandenv` regardless of registration order
4. Override embedder functions (`RegisterFuncsOverride`) -- applied
   unconditionally, and the only way to deliberately re-enable a stripped
   `env`/`expandenv`

## Fail-loud wiring

When an embedder supplies `RootOptions.GoTemplateFuncs`, a collision is treated
as an embedder build bug: the root command reports the error and exits via the
writer's exit path rather than dropping the function and continuing. This keeps
the failure obvious at startup instead of surfacing as a confusing "function not
defined" template error later. The wiring registers exactly once per
`RootOptions`, so a command executed more than once (a REPL or long-running
host) does not re-register the process-global functions and self-collide.

## Discoverability

Registered functions are tagged with `Source: "embedder"`
(`gotmpl.SourceEmbedder`) and surface through the normal discovery tools:

- CLI: `mycli get template functions --embedder`
- MCP: `list_go_template_functions` with `embedder_only: true`

The default (unfiltered) views also include embedder functions alongside the
sprig (`source: sprig`) and custom (`source: custom`) built-ins. When an
embedder shadows a built-in via `RegisterFuncsOverride`, the combined listing is
de-duplicated by name (`gotmpl.CombinedFuncs`) so only the effective embedder
function is shown, keeping discovery aligned with what actually runs.

## Related

- Package reference: `pkg/gotmpl/README.md` ("Embedder Function Registration")
- Other embedder hooks on `RootOptions`: `BuiltinAuthHandlers`,
  `ClusterResolver`, `OfficialProviders`, `PluginSignaturePolicy`

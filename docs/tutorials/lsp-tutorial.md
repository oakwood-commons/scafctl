---
title: "Language Server (LSP) Tutorial"
weight: 47
---

# Language Server (LSP) Tutorial

This tutorial covers `scafctl lsp`, which runs a Language Server Protocol (LSP)
server so editors can surface solution diagnostics as you type.

## Overview

`scafctl lsp` speaks LSP over stdin/stdout. An editor / LSP client launches it,
sends the documents you open and edit, and the server replies with diagnostics.
It is not meant to be run interactively -- stdout is the JSON-RPC channel.

```mermaid
flowchart LR
  A["Editor<br/>(LSP client)"] <-->|"stdio JSON-RPC"| B["scafctl lsp"]
  B --> C["lint engine"]
  C -->|"findings"| B
  B -->|"publishDiagnostics"| A
```

Today the server publishes **lint diagnostics**: on open/change/save it lints
the in-memory document and reports each finding (severity, message, rule name)
at its source location, using the same engine as `scafctl lint` -- so what you
see in the editor matches the CLI.

It also supports resolver, action, call, and function **navigation and rename**,
reusing the same reference index and rename engine as `scafctl refactor rename`:

- **Go to definition** on a resolver, action, call, or function reference jumps
  to where it is defined.
- **Find all references** lists every use of a resolver, action, call, or
  function.
- **Rename** any of them updates its definition and every reference in one edit.
  If any reference cannot be located, the rename is refused (the editor shows the
  error) rather than applying a partial, solution-breaking change.

It also provides a **document outline**. `textDocument/documentSymbol` returns a
hierarchy -- a `spec` root whose children are the `resolvers`, `actions`,
`calls`, and `functions` groups, each listing its symbols -- so the editor's
Outline pane and breadcrumbs populate and **Go to Symbol in File**
(Cmd/Ctrl+Shift+O) fuzzy-jumps to any resolver, action, call, or function.

## Trying it by hand

You normally never run the server directly, but you can confirm it starts:

```bash
scafctl lsp --help
```

An LSP client drives it with framed JSON-RPC messages (`initialize`, then
`textDocument/didOpen`, `didChange`, `didClose`). On `didOpen`/`didChange` the
server responds with a `textDocument/publishDiagnostics` notification containing
any lint findings for that document.

## Configuring an editor

The exact steps depend on the editor's LSP client, but the shape is always the
same:

1. Point the client at the `scafctl lsp` command (the server process). Clients
   that pass a transport flag by convention can use `scafctl lsp --stdio`; it is
   accepted as a no-op since stdio is the only transport.
2. Attach it to the solution files scafctl recognizes. To avoid hardcoding a
   file list that drifts from CLI discovery, ask the binary which files it
   auto-discovers:

   ~~~bash
   scafctl lsp document-selectors -o json
   ~~~

   This reports the recognized file names partitioned by editor language --
   `solution.{yaml,yml,json}`, `<binary>.{yaml,yml,json}`, `taskfile.{yaml,yml}`,
   and `actions.{yaml,yml}` -- along with the effective binary name. JSON
   solutions are listed separately so the client attaches them as JSON (not
   YAML) documents.
3. Open a `solution.yaml` (or `solution.json`, `taskfile.yaml`, ...) --
   diagnostics appear inline as you edit.

The bundled VS Code extension does this automatically: it queries
`scafctl lsp document-selectors` at startup and builds its document selector
from the result, so it always matches CLI auto-discovery -- including any
embedder binary name. Users can add globs for non-standard file names via the
`scafctl.solutionFilePatterns` setting.

Because the server reuses the CLI's `lint` engine, no separate configuration is
needed to keep editor and CLI diagnostics consistent.

## What's next

Navigation and rename currently cover resolvers. Planned additions extend the
same reference index to actions, functions, and calls, and add hover and
completion.

## See also

- [Linting Tutorial](linting-tutorial.md) -- the diagnostics engine behind the server
- [Refactoring Tutorial](refactor-tutorial.md) -- the reference index future LSP features will reuse

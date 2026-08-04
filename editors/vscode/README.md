# scafctl for VS Code

Language support for [scafctl](https://github.com/oakwood-commons/scafctl)
solution files: diagnostics, go-to-definition, find references, and rename --
powered by the `scafctl lsp` language server.

## Features

For `solution.yaml` / `scafctl.yaml` files:

- **Diagnostics** -- lint findings appear inline as you edit, matching `scafctl lint`.
- **Go to Definition** -- jump from a resolver reference to its definition.
- **Find All References** -- list every use of a resolver.
- **Rename Symbol** -- rename a resolver and every reference to it in one edit. If
  a reference cannot be located, the rename is refused rather than applied
  partially.

The feature set grows automatically as the language server gains capabilities --
the extension is a thin client.

## Requirements

The extension runs the `scafctl` CLI as its language server. Install scafctl and
make sure it is on your `PATH`, or set `scafctl.serverPath` to its location.

If `scafctl` cannot be found, the extension shows an error with a link to
Settings; it does not download anything.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `scafctl.serverPath` | `""` | Path to the `scafctl` executable. Empty resolves `scafctl` from `PATH`. |
| `scafctl.trace.server` | `off` | Trace LSP traffic (`off` / `messages` / `verbose`). |

## Commands

- **scafctl: Restart Language Server** -- restart the server (for example after
  updating the `scafctl` binary).

## Development

This extension lives in the scafctl monorepo under `editors/vscode`.

```bash
npm install        # or: task vscode:install
npm run check      # type-check
npm run lint       # eslint
npm test           # unit tests (set SCAFCTL_BIN to also check a real binary)
npm run build      # esbuild bundle -> out/extension.js
npm run package    # produce scafctl.vsix
```

Or via the repo task runner: `task vscode:build`, `task vscode:package`.

To run the extension against a locally built server, `task build` the CLI and set
`scafctl.serverPath` to `./dist/scafctl`, then launch the Extension Development
Host (F5).

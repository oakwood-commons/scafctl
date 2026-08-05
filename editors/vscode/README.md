# scafctl for VS Code

Language support for [scafctl](https://github.com/oakwood-commons/scafctl)
solution files: diagnostics, go-to-definition, find references, and rename --
powered by the `scafctl lsp` language server.

## Features

For scafctl solution and action files:

- **Diagnostics** -- lint findings appear inline as you edit, matching `scafctl lint`.
- **Go to Definition** -- jump from a resolver reference to its definition.
- **Find All References** -- list every use of a resolver.
- **Rename Symbol** -- rename a resolver and every reference to it in one edit. If
  a reference cannot be located, the rename is refused rather than applied
  partially.

The feature set grows automatically as the language server gains capabilities --
the extension is a thin client.

### Which files get language features

The extension attaches the language server to exactly the files the `scafctl`
binary auto-discovers -- it queries the binary at startup (via
`scafctl lsp document-selectors`) instead of hardcoding a list, so editor
targeting never drifts from CLI discovery. Out of the box this covers, in any
folder:

- `solution.yaml` / `solution.yml` / `solution.json`
- `<binary>.yaml` / `<binary>.yml` / `<binary>.json` (e.g. `scafctl.yaml`; for an
  embedding CLI this is that CLI's name)
- `taskfile.yaml` / `taskfile.yml`
- `actions.yaml` / `actions.yml`

JSON solution files are attached as JSON documents (not YAML), so language
features work regardless of the on-disk format.

For a differently named file, either **rename it to a standard name** (simplest
-- this also enables `scafctl` CLI auto-discovery), or add a glob to
`scafctl.solutionFilePatterns` (see Settings).

## Requirements

The extension runs the `scafctl` CLI as its language server. Install scafctl and
make sure it is on your `PATH`, or set `scafctl.serverPath` to its location.

If `scafctl` cannot be found, the extension shows an error with a link to
Settings; it does not download anything.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `scafctl.serverPath` | `""` | Path to the `scafctl` executable. Empty resolves `scafctl` from `PATH`. |
| `scafctl.solutionFilePatterns` | `[]` | Extra glob patterns for solution files with non-standard names (e.g. `**/my-solution.yaml`). By default these **extend** the auto-discovered set. |
| `scafctl.solutionFilePatterns.replaceDefaults` | `false` | When `true`, the patterns above **replace** the auto-discovered defaults instead of extending them. |
| `scafctl.language.enable` | `false` | Opt in to a dedicated `scafctl` language mode (see below). |
| `scafctl.trace.server` | `off` | Trace LSP traffic (`off` / `messages` / `verbose`). |

Changing any of these recreates the language client automatically -- no window
reload needed.

Server logs and (when `scafctl.trace.server` is `messages`/`verbose`) JSON-RPC
traces appear in the **scafctl** Output channel; it is revealed automatically if
the server reports an error.

### The `scafctl` language mode (opt-in)

By default, solution files keep their built-in `yaml` / `json` language. This is
deliberate: it preserves other extensions' features on those files -- most
notably the Red Hat YAML extension's schema validation, autocompletion, and
formatting -- while the scafctl language server adds its diagnostics and
navigation on top. This matches how tools like GitHub Actions layer over YAML.

Enabling `scafctl.language.enable` contributes a dedicated `scafctl` language
(YAML syntax highlighting, scafctl diagnostics). It does **not** re-associate
files automatically; you opt a file in by setting its language mode to `scafctl`
(or via `files.associations`). Note that switching a file to the `scafctl`
language turns off the YAML/JSON extensions' schema features for that file, so
leave this off unless you specifically want it.

> Non-standard file names only receive features once the extension is active. It
> activates when a workspace contains a standard solution file, or when any file
> is set to the `scafctl` language. If your workspace has only custom-named files
> on the built-in `yaml`/`json` language, run **scafctl: Restart Language Server**
> once to activate it.

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

## Releasing

The extension is published to **two** registries from one `.vsix`: the Visual
Studio Marketplace (via `vsce`) and the Open VSX Registry (via `ovsx`, used by
VSCodium, Cursor, Windsurf, etc.).

### One-time setup

- **`VSCE_PAT`** -- an Azure DevOps PAT with the *Marketplace > Manage* scope for
  the `oakwood-commons` publisher. Add it as a repo secret.
- **`OVSX_PAT`** -- an Open VSX access token. The `oakwood-commons` namespace must
  exist first (`npx ovsx create-namespace oakwood-commons -p <token>`). Add it as
  a repo secret.

### Automated (recommended)

Bump the version and push a `vscode/v*` tag; the
[`VS Code Extension Release`](https://github.com/oakwood-commons/scafctl/blob/main/.github/workflows/vscode-release.yml) workflow
verifies the tag matches `package.json`, builds/tests/packages, and publishes to
both registries.

```bash
cd editors/vscode
npm version patch --no-git-tag-version   # or: minor / major
# commit the version bump (signed + DCO), then:
git tag vscode/v0.1.1                     # tag must equal the new package.json version
git push origin vscode/v0.1.1
```

### Manual

```bash
cd editors/vscode
npm ci
npm version patch --no-git-tag-version
npm run package                           # -> scafctl.vsix
VSCE_PAT=... npm run publish:vsce         # Visual Studio Marketplace
OVSX_PAT=... npm run publish:ovsx         # Open VSX Registry
```

Or run both publishes via the task runner: `task vscode:publish` (reads
`VSCE_PAT` / `OVSX_PAT` from the environment).

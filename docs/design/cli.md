---
title: "CLI"
weight: 8
---

# CLI Usage

This document describes how to invoke `scafctl` from the command line. The CLI follows a `kubectl`- and `docker`-inspired hybrid structure where core domain workflows use `<verb> <kind>` and supporting subsystems use `<noun> <verb>`.

The general pattern for core domain operations is:

~~~text
scafctl <verb> <kind> [name[@version]] [key=value...] [flags]
~~~

- `<verb>` describes what you want to do (`run`, `render`, `inspect`, `get`, `explain`, `package`, `diff`, `validate`, `new`)
- `<kind>` identifies the resource type (`solution`, `action`, `resolver`, `provider`, `bundle`, `snapshot`, `schema`)
- `<name>` identifies a catalog, OCI registry, or URL artifact
- `@version` is optional for exact semver (`@1.0.0`) or digest (`@sha256:...`) pins; use `--version` (`--version "^1.2"`, `--version ">=1.0 <2.0"`) for semver range constraints

---

## Implementation Status

| Command | Status | Notes |
|---------|--------|-------|
| `run solution` | Implemented | Executes resolvers + workflow actions (`-a/--action` filters actions; accepts positional `key=value` params) |
| `run action` | Implemented | Executes named workflow actions (`-s/--solution` for catalog/remote/URL, `-f/--file` for local files, `-l/--list` to list actions) |
| `run resolver` | Implemented | Resolver-only execution for debugging and inspection (`-s/--solution` for catalog/remote/URL, `-a/--action` scope, `--show-execution`, `--graph`, `--snapshot`) |
| `run provider` | Implemented | Executes a single provider directly for isolated testing and debugging |
| `render solution` | Implemented | Renders executor-ready action graphs, `--action-graph`, `--effective` post-compose YAML, or `--snapshot --snapshot-file` |
| `get solution/provider/examples/snapshot/cel/template/commands` | Implemented | Lists resources or shows a single resource (`get examples` lists and prints embedded examples) |
| `explain <kind>` | Implemented | Schema documentation for resource kinds (`solution`, `provider`, `action`, `resolver`, `workflow`, `spec`, `valueref`, `tokenref`, etc.) |
| `inspect solution [--usage]` | Implemented | Instance structure; `--usage` renders the consumer-facing synopsis, parameters, and runnable action commands |
| `diff solution/bundle/snapshot` | Implemented | Structural comparison of solutions, bundled solution versions, or execution snapshots |
| `package solution/plugin` | Implemented | Packages solutions or multi-platform plugins into the local OCI catalog (`build` alias retained) |
| `extract bundle` | Implemented | Lists or extracts files from a bundled solution artifact (`--list-only`, `--output-dir`, `--resolver`, `--action`) |
| `vendor update` | Implemented | Re-resolves and updates vendored catalog dependencies and `solution.lock` |
| `catalog *` | Implemented | `list`, `inspect`, `push`, `pull`, `delete`, `prune`, `save`, `load`, `tags`, `login`, `logout`, `remote` (`add`, `remove`, `default`, `list`), `attach`, `index` |
| `config *` | Implemented | `init`, `view`, `get`, `set`, `unset`, `paths`, `schema`, `validate`, `reset` |
| `secrets *` | Implemented | `list`, `get`, `set`, `delete`, `exists`, `export`, `import`, `rotate` |
| `state *` | Implemented | `list`, `show`, `get`, `set`, `delete`, `clear`, `fingerprints` |
| `auth *` | Implemented | `login`, `logout`, `status`, `list`, `token`, `handlers`, `profile`, `switch`, `alias`, `oauth2`, `diagnose`, `migrate` |
| `kube *` | Implemented | `login`, `logout`, `list`, `status` |
| `cache *` | Implemented | `info`, `clear` (aliases `clean`, `rm`) |
| `credential-helper *` | Implemented | `install`, `uninstall`, `get`, `store`, `erase`, `list` |
| `plugins *` | Implemented | `install`, `list`, `update`, `prune` |
| `mcp *` | Implemented | `init`, `serve`, `list` |
| `serve` / `serve openapi` | Implemented | Starts the REST API server or exports the OpenAPI specification |
| `lsp` / `lsp document-selectors` | Implemented | Runs the Language Server Protocol server over stdio and prints editor file-selector globs |
| `eval cel/template/validate/refs` | Implemented | Evaluates, validates syntax, or extracts resolver references from CEL expressions and Go templates |
| `validate` / `validate solution/resolver/schema` | Implemented | Primary pass/fail gate: loads and lints solutions (`--strict` makes warnings fatal), validates resolvers, or checks JSON Schema conformance |
| `lint` / `lint solution/rules/rule` | Implemented | Advisory authoring diagnostics, rule catalog (`lint rules`, `lint rule <id>`), and `--fix` auto-fixes |
| `test` / `test functional/init/list` | Implemented | Functional test execution (`test` without subcommand runs `test functional` with full flag parity) |
| `refactor rename` | Implemented | Source-preserving rename across `resolver`, `action`, `call`, and `function` symbols |
| `new solution` | Implemented | Scaffolds a new solution YAML from built-in templates |
| `-c` / `--catalog` flag | Implemented | Targets a configured catalog name or OCI registry URL (`run`, `render`, `validate resolver`, `catalog *`) |
| Version constraints (`--version "^1.2"`) | Implemented | Resolves semver ranges (`^1.2`, `~2.1`, `>=1.0 <2.0`) via `--version` on CLI commands (and `@^1.2` / `version: "^1.2"` in YAML dependencies) |
| `resolver graph` | Removed | Use `run resolver --graph` instead |

---

## Core Concepts

### Kinds

- **Catalog artifact kinds**: `solution`, `provider`, `auth-handler` (`--kind plugin` on `catalog list` selects both `provider` and `auth-handler`)
- **Execution and schema kinds**: `solution`, `action`, `resolver`, `provider`, `bundle`, `snapshot`, `schema`

---

### Names and Versions

Names identify an object within a kind. Versions are optional and may be:

- an exact semantic version or digest inline (`example@1.0.0`, `example@sha256:...`)
- a semver constraint via `--version` (`--version "^1.2"`, `--version "~2.1"`, `--version ">=1.0 <2.0"`) -- resolved to the highest matching version in the target catalog (note: inline `@^1.2` syntax is used in YAML sub-solution `source:` fields, whereas CLI commands take exact versions after `@` and ranges via `--version`)
- omitted (resolves to the highest stable semantic version available; pass `--pre-release` to include pre-releases)

**Shell escaping**: Quote version constraints that contain spaces or shell operators (`>`, `<`):

~~~bash
scafctl run solution example@1.0.0
scafctl run solution example --version "^1.2"
scafctl run solution example --version ">=1.0 <2.0"
~~~

### File Paths vs. Catalog References

When a command accepts both a positional reference argument and a `-f`/`--file` flag, the CLI strictly separates catalog/remote references from local filesystem paths:

| Input | Interpretation | Example |
|-------|---------------|---------|
| Bare name | Catalog reference | `my-app`, `deploy` |
| Versioned name | Catalog reference (exact version or digest) | `my-app@1.0.0`, `deploy@latest` |
| Registry reference | OCI catalog reference | `ghcr.io/my-org/solutions/sol@1.0.0`, `localhost:5000/solutions/sol@1.0.0` |
| URL | Remote HTTP(S) reference | `https://example.com/sol.yaml` |
| Starts with `/` or `.` | **Rejected** -- use `-f` | `/tmp/sol.yaml`, `./sol.yaml` |
| Ends with `.yaml`, `.yml`, `.json` | **Rejected** -- use `-f` | `solution.yaml` |
| Path with separators, non-hostname first segment | **Rejected** -- use `-f` | `configs/solution`, `relative/path/sol` |
| Windows path | **Rejected** -- use `-f` | `C:\dir\sol`, `dir\sol` |

To pass a local file path, always use the `-f`/`--file` flag:

~~~bash
# Correct: local file via -f flag
scafctl run solution -f ./solution.yaml

# Correct: catalog reference as positional arg
scafctl run solution my-app@1.0.0

# Error: local file path as positional arg
scafctl run solution solution.yaml   # rejected with helpful error

# Error: catalog reference passed to -f/--file
scafctl run solution -f my-app       # rejected with helpful error
~~~

This separation keeps the CLI unambiguous -- positional arguments on solution-targeted commands are catalog/registry/URL lookups while `-f/--file` is strictly a local file path (or `-` for stdin). On commands whose positional arguments name actions or resolvers (`run action`, `run resolver`, `validate resolver`), pass catalog, OCI registry, or URL solution references via `-s`/`--solution`:

~~~bash
# Run a specific action from a catalog solution
scafctl run action init-repo -s my-app
scafctl run solution my-app -a init-repo

# Run specific resolvers from a catalog solution
scafctl run resolver db config -s my-app@1.0.0
~~~

### Resource Subcommand Aliases

Every `solution` subcommand (`run`, `render`, `inspect`, `validate`, `lint`, `get`, `package`, `diff`, `new`, `catalog push/pull/delete`) supports the aliases `sol`, `s`, and `solutions`. Top-level `plugins` also aliases `plugin` (matching `secrets` / `secret`).

---

## Running Solutions and Actions

### Running a Solution (`run solution`)

Execute a solution's resolvers and perform its workflow actions. The solution **must** define a `workflow` section with actions -- if no workflow is defined, the command errors and suggests using `scafctl run resolver` instead.

~~~bash
# Run latest version from catalog
scafctl run solution example

# Run a specific version or semver constraint
scafctl run solution example@1.0.0
scafctl run solution example --version "^1.2"

# Run from a local file with positional key=value parameters
scafctl run solution -f ./solution.yaml env=prod region=us-east1

# Run only selected workflow actions (and their transitive dependsOn actions)
scafctl run solution example -a build -a test
~~~

### Running Specific Actions (`run action`)

`run action` executes named workflow actions directly (defaulting auto-discovery to `actions.yaml` before `solution.yaml`):

~~~bash
# List available actions without executing
scafctl run action --list
scafctl run action -l -o json

# Run named actions from auto-discovered actions.yaml / solution.yaml
scafctl run action lint test

# Run an action from a local file
scafctl run action build -f ./solution.yaml version=1.0.0

# Run an action from a catalog or OCI registry solution
scafctl run action deploy -s my-app@1.2.0 env=prod
~~~

---

## Getting Resources (`get`)

Show metadata of a catalog solution or list available resources:

~~~bash
# Get latest or specific version from catalog
scafctl get solution example
scafctl get solution example@1.0.0

# Inspect auto-discovered local solution summary
scafctl get solution --local
~~~

### Listing Resources

Following `kubectl` conventions, singular and plural forms work interchangeably:

~~~bash
# List all solutions in the catalog
scafctl get solutions

# List all providers (built-in + official)
scafctl get providers
scafctl get providers -i

# Inspect a single operation on an operation-based provider
scafctl get provider github --operation create_issue

# List CEL extension functions or Go template functions
scafctl get cel functions
scafctl get template functions

# List all CLI commands as structured data
scafctl get commands -o json
~~~

The `get providers` output includes a `source` column indicating whether each provider is `builtin` (compiled in) or `official` (auto-fetched from the OCI catalog). When filters (`--capability`, `--category`) are active, only built-in providers are shown since official providers do not expose capability/category metadata before download.

---

## Rendering a Solution (`render solution`)

`render solution` executes resolvers and builds an executor-ready action graph without performing action side effects.

~~~bash
# From catalog (by name or version)
scafctl render solution example
scafctl render solution example@1.0.0

# From local file or stdin
scafctl render solution -f mysolution.yaml
cat solution1.yaml | scafctl render solution -f -

# Write rendered output to a file instead of stdout
scafctl render solution -f mysolution.yaml -o yaml --output-file rendered.yaml

# Visualize action dependency graph (ascii, dot, mermaid, json)
scafctl render solution -f mysolution.yaml --action-graph --graph-format mermaid

# Emit the effective (post-compose) solution YAML without running resolvers
scafctl render solution -f mysolution.yaml --effective -o yaml
~~~

> [!NOTE]
> The `-f`/`--file` flag is used consistently across `run`, `render`, `inspect`, `validate`, `lint`, `test`, `package`, and `diff` to indicate a local file path rather than a catalog lookup.

---

## Passing Resolver Parameters

Resolver parameters can be passed positionally (`key=value`, `@file`, `@-`) after the target name on `run solution`, `run action`, `run resolver`, `run provider`, and `validate resolver`, or explicitly using `-r` / `--resolver`:

~~~bash
scafctl run solution example env=prod
scafctl run solution example -r env=prod
~~~

Multiple parameters:

~~~bash
scafctl run solution example \
  -r env=prod \
  -r region=us-east1
~~~

Parameters participate in normal resolver resolution via the `parameter` provider.

### Key-Value Format

Resolver parameters use a `key=value` format where:

- **Key**: Simple string identifier (no spaces or newlines allowed)
- **Value**: Supports all characters including newlines, special characters, and quotes

#### Basic Usage

~~~bash
scafctl run solution example \
  -r someKey=sk_live_abc123 \
  -r config='{"nested": "json"}' \
  -r script="line1
line2
line3"
~~~

#### CSV Support

Pass multiple comma-separated `key=value` pairs in a single `-r` flag:

~~~bash
# Multiple pairs in one flag
scafctl run solution example \
  -r "env=prod,region=us-east1,region=us-west1"

# Quoted values preserve commas as literal characters
scafctl run solution example \
  -r "msg=\"Hello, world\"" \
  -r "data='item1,item2,item3'"

# Escaped quotes within quoted values
scafctl run solution example \
  -r "json=\"{\\\"key\\\":\\\"value\\\"}\""
~~~

#### Multiple Values for Same Key

Values for the same key are automatically combined into an array, whether using CSV or repeated flags:

~~~bash
# Repeated flags
scafctl run solution example \
  -r region=us-east1 \
  -r region=us-west1 \
  -r region=eu-west1

# CSV in one flag
scafctl run solution example \
  -r "region=us-east1,region=us-west1,region=eu-west1"

# Mixed approach (all three produce: region = [us-east1, us-west1, eu-west1])
scafctl run solution example \
  -r "region=us-east1,region=us-west1" \
  -r region=eu-west1
~~~

**Technical Note**: The CLI uses `StringArrayVarP` with custom CSV parsing (`pkg/flags.ParseKeyValueCSV`) to avoid Cobra's built-in CSV issues while supporting comma-separated values with quote handling.

#### URI Scheme Support

To pass structured or encoded content without shell-escaping overhead, use URI scheme prefixes:

**Supported schemes**: `json://`, `yaml://`, `base64://`, `http://`, `https://`, `file://`

~~~bash
# JSON without quote escaping
scafctl run solution example \
  -r "config=json://{\"key\":\"value\",\"count\":42}"

# JSON with commas in CSV context
scafctl run solution example \
  -r "env=prod,data=json://[1,2,3],region=us-east1"

# YAML configuration
scafctl run solution example \
  -r "config=yaml://items: [a, b, c]"

# Base64 encoded data
scafctl run solution example \
  -r "token=base64://SGVsbG8sIFdvcmxkIQ=="
~~~

Values with URI schemes are validated immediately (`json://` as JSON, `yaml://` as YAML, `base64://` as base64, `file://` as an existing non-directory file, and `http://`/`https://` as valid URLs). The scheme prefix is preserved for your solution logic.

#### Parameter File References (`@file`) and Stdin (`@-`)

Load a map of parameters from a YAML or JSON file (or stdin) using `@`:

~~~bash
# Load from a YAML or JSON file
scafctl run resolver -f solution.yaml -r @params.yaml
scafctl run resolver -f solution.yaml @params.json

# Mix file and inline parameters
scafctl run resolver -f solution.yaml -r @defaults.yaml env=prod

# Read YAML/JSON parameter map from stdin
echo '{"env": "prod", "region": "us-east1"}' | scafctl run resolver -f solution.yaml @-
cat params.yaml | scafctl run solution example -r @-
~~~

#### Per-Key Stdin and File References (`key=@-` / `key=@file`)

Assign raw content from stdin or a file directly to a single key (a single trailing newline is trimmed automatically):

~~~bash
# Pipe raw text into a single parameter
echo hello | scafctl run provider message message=@-
echo hello | scafctl run resolver -f solution.yaml -r message=@-

# Read a file's raw content into a parameter
scafctl run provider http url=https://example.com body=@request.json
scafctl run resolver -f solution.yaml -r config=@defaults.txt
~~~

**Restrictions:**

- `@-` can only appear once per invocation (applies across standalone `@-` and `key=@-`)
- `@-` cannot be combined with `-f -` (both consume stdin)

---

## Execution Snapshots and Diffing (`diff`)

Capture resolver execution state for testing, auditing, and drift comparison:

~~~bash
# Save snapshot via render solution or run resolver
scafctl render solution -f solution.yaml --snapshot --snapshot-file output.json
scafctl render solution -f solution.yaml --snapshot --snapshot-file output.json --redact
scafctl run resolver -f solution.yaml --snapshot --snapshot-file output.json --redact

# Display a saved snapshot
scafctl get snapshot output.json
~~~

Compare solutions, bundles, and snapshots through the unified `diff` command group:

~~~bash
# Structural diff between two solution files or catalog versions
scafctl diff solution -f solution-v1.yaml -f solution-v2.yaml
scafctl diff solution my-app@1.0.0 my-app@2.0.0

# Compare two bundled solution versions (YAML, bundled files, vendored deps, plugins)
scafctl diff bundle my-app@1.0.0 my-app@2.0.0
scafctl diff bundle my-app@1.0.0 my-app@2.0.0 --files-only

# Compare two resolver execution snapshots
scafctl diff snapshot before.json after.json --ignore-unchanged
~~~

---

## Packaging, Bundling, and Working With Catalogs

`scafctl` ships a full local and remote OCI catalog workflow for packaging, signing, distributing, and running solutions and multi-platform plugins.

### Packaging Artifacts (`package` and `vendor`)

Package a solution or plugin into the local OCI catalog (`build` is retained as a backward-compatible alias for `package`):

~~~bash
# Package a solution (uses metadata.name and metadata.version by default)
scafctl package solution -f ./solution.yaml
scafctl package solution -f ./solution.yaml --version 1.0.0 --force

# Preview discovered files, bundle.include matches, and plugin pins without storing
scafctl package solution -f ./solution.yaml --dry-run

# Emit a machine-readable JSON build report on stdout (progress goes to stderr)
scafctl package solution -f ./solution.yaml -o json

# Skip file bundling or dependency vendoring, or cap bundle size
scafctl package solution -f ./solution.yaml --no-bundle
scafctl package solution -f ./solution.yaml --no-vendor
scafctl package solution -f ./solution.yaml --bundle-max-size 100MB

# Package a multi-platform provider or auth-handler plugin
scafctl package plugin --name aws-provider --kind provider --version 1.0.0 \
  --platform linux/amd64=./dist/aws-provider-linux-amd64 \
  --platform darwin/arm64=./dist/aws-provider-darwin-arm64
~~~

See [catalog-build-bundling.md](../design/catalog-build-bundling.md) for the multi-layer bundle architecture. To re-resolve and upgrade vendored sub-solutions and locked plugins in `solution.lock` without manually editing the lockfile, use `vendor update`:

~~~bash
scafctl vendor update --dry-run
scafctl vendor update
scafctl vendor update --dependency provider/exec
~~~

To inspect or extract files packaged inside a bundled solution artifact, use `extract bundle`:

~~~bash
scafctl extract bundle my-solution@1.0.0 --list-only
scafctl extract bundle my-solution@1.0.0 --output-dir ./extracted
~~~

### Publishing Artifacts (`catalog push` and `catalog attach`)

Push packaged artifacts from the local catalog to any OCI-compliant registry. Solutions automatically generate and attach an **SPDX 2.3 JSON SBOM** OCI referrer unless `--no-sbom` is set:

~~~bash
# Push using a short reference and target catalog (-c/--catalog auto-detects kind when unique)
scafctl catalog push my-solution@1.7.0 -c ghcr.io/my-org

# Push using a full OCI reference
scafctl catalog push ghcr.io/my-org/solutions/my-solution@1.7.0

# Disambiguate kind explicitly or push to the default configured catalog
scafctl catalog push solution my-solution@1.7.0 -c production
scafctl catalog push provider aws-provider@1.5.0 -c production
scafctl catalog push auth-handler custom-sso@1.0.0 -c production

# Tag as 'latest' in the remote registry or publish under an alias name@version
scafctl catalog push my-solution@1.7.0 -c ghcr.io/my-org --latest
scafctl catalog push ghcr.io/my-org/solutions/my-solution@1.7.0 --as release-solution@1.7.0

# Attach additional OCI referrer artifacts (provenance, attestations, custom SBOMs)
scafctl catalog attach my-solution@1.7.0 --file provenance.intoto.json \
  --type application/vnd.in-toto+json -c ghcr.io/my-org
~~~

### Pulling and Listing Remote Tags (`catalog pull` and `catalog tags`)

~~~bash
# Pull using a full OCI reference
scafctl catalog pull ghcr.io/my-org/solutions/example@1.7.0

# Pull using kind subcommands (resolves against configured catalogs)
scafctl catalog pull solution example@1.7.0
scafctl catalog pull provider aws-provider@1.5.0
scafctl catalog pull auth-handler github@1.0.0

# Store locally under a different concrete name@version
scafctl catalog pull ghcr.io/my-org/solutions/example@1.7.0 --as local-example@1.7.0

# List semver and alias tags on a remote registry
scafctl catalog tags ghcr.io/my-org/solutions/example
~~~

### Inspecting, Listing, and Indexing Catalogs

~~~bash
# List catalog entries (default catalog + official fallback; --all queries all configured catalogs)
scafctl catalog list
scafctl catalog list --kind plugin
scafctl catalog list -c production

# Inspect artifact metadata and attached OCI referrers (SBOMs, Cosign signatures)
scafctl catalog inspect example@1.7.0
scafctl catalog inspect example@1.7.0 --referrers -c ghcr.io/my-org
scafctl catalog inspect example@1.7.0 --referrers --artifact-type application/spdx+json -c ghcr.io/my-org

# Build/push or display a catalog discovery index for registries without tag-enumeration permissions
scafctl catalog index push -c ghcr.io/my-org
scafctl catalog index show -c ghcr.io/my-org
~~~

### Offline Distribution (`catalog save` and `catalog load`)

Export and import OCI Image Layout tar archives for air-gapped environments:

~~~bash
scafctl catalog save my-solution@1.2.3 -o solution.tar
scafctl catalog load --input solution.tar
scafctl catalog load --input solution.tar --force
~~~

### Deleting and Pruning Artifacts

~~~bash
# Delete a specific local version and prune orphaned blobs
scafctl catalog delete solution example@1.7.0
scafctl catalog prune

# Delete all local catalog artifacts
scafctl catalog delete --all --force

# Delete from a remote registry (where OCI DELETE is supported)
scafctl catalog delete ghcr.io/my-org/solutions/example@1.7.0
~~~

### Targeting a Specific Catalog (`-c` / `--catalog`)

When resolving a bare solution name (`run solution example`, `render solution example`), `scafctl` checks matching locally built solutions in the `local` catalog first (a matching local build wins even if a newer remote version exists) and otherwise consults **only** your configured default remote catalog (`settings.defaultCatalog`) -- it does **not** fall back to the `official` catalog. If no matching local build exists and no default remote catalog is configured, bare-name lookup fails. Configure a default catalog (`scafctl catalog remote default <name>`) or pass `-c` / `--catalog` explicitly on `run`, `render`, `validate resolver`, or `catalog` subcommands to target a desired catalog or registry URL (including `-c official`):

~~~bash
scafctl run solution example -c internal
scafctl run solution example -c official
scafctl run action deploy -s example -c internal
scafctl render solution example -c ghcr.io/my-org
scafctl catalog list -c production
~~~

---

## Inspecting and Explaining Resources

`scafctl` separates schema documentation from instance inspection (see `docs/design/information-command-taxonomy.md`):

- `explain <kind>` -- schema documentation for a resource *kind* (from Go struct tags), e.g. `explain solution`, `explain provider`, `explain action`, `explain resolver`, `explain valueref`. Drill into nested fields with `explain provider.schema`.
- `inspect <resource>` -- structure and metadata of a specific *instance* (kvx-native), e.g. `inspect solution`.

### Inspect Solution (structure and `--usage`)

~~~bash
# Developer structure view (from local file or catalog)
scafctl inspect solution -f solution.yaml
scafctl inspect solution example@1.0.0 -o json

# Consumer-facing "how do I run this?" usage view
scafctl inspect solution -f solution.yaml --usage
scafctl inspect solution example --usage -o json
~~~

Authors can enrich `--usage` with an optional `metadata.usage` block (`synopsis`, `details`, and `examples` containing `description`/`command` pairs) alongside parameter `description`/`example` fields and action `description` fields.

### Explain Schema Kinds

~~~bash
scafctl explain solution
scafctl explain provider
scafctl explain provider.schema
scafctl explain action --recursive
scafctl explain resolver
~~~

---

## Global and Common Per-Command Flags

Run `scafctl options` to list all global flags inherited by every command:

| Flag | Short | Description | Status |
|------|-------|-------------|--------|
| `--cwd` | `-C` | Change the working directory before executing the command (similar to `git -C`) | Implemented |
| `--quiet` | `-q` | Do not print additional informational output | Implemented |
| `--verbose` | | Enable verbose diagnostic output across all commands | Implemented |
| `--no-color` | | Disable colored output | Implemented |
| `--config` | | Path to config file (default: `$XDG_CONFIG_HOME/scafctl/config.yaml` or `~/.config/scafctl/config.yaml`) | Implemented |
| `--log-level` | | Set log level (`none`, `error`, `warn`, `info`, `debug`, `trace`, or numeric V-level; default `none`) | Implemented |
| `--debug` | `-D` | Enable debug logging (shorthand for `--log-level debug`) | Implemented |
| `--log-format` | | Set log output format (`console`, `json`; default `console`) | Implemented |
| `--log-file` | | Write logs to a file instead of stderr | Implemented |
| `--auth-profile` | | Override the active auth profile for all auth operations (e.g. `work`, `personal`) | Implemented |
| `--otel-endpoint` | | OpenTelemetry OTLP exporter endpoint (overrides `OTEL_EXPORTER_OTLP_ENDPOINT`) | Implemented |
| `--otel-insecure` | | Disable TLS for OTLP gRPC connection (development only) | Implemented |

### Common Per-Command Flag Conventions

- **`-o` / `--output` (format) vs. `--output-file` (path)**: On commands with structured output (`run`, `render`, `get`, `inspect`, `diff`, `validate`, `lint`, `test`, `eval`, `catalog`, `config view`, `secrets export`, `serve openapi`, `mcp`), `-o` / `--output` selects the **data format** (`auto`, `table`, `json`, `yaml`, `quiet`, etc.) while `--output-file` writes output to a **file path** instead of stdout (`render solution`, `new solution`, `secrets export`, `serve openapi`). Note: `catalog save -o <file.tar>` uses `-o` for the archive destination.
- **`-i` / `--interactive` and `-e` / `-w` filtering**: `kvx`-backed commands support `-i` for interactive TUI browsing, `-e` / `--expression` for CEL projection over `_`, and `-w` / `--where` for per-item boolean filtering.
- **`-c` / `--catalog`**: Per-command flag on `run`, `render`, `validate resolver`, and `catalog` commands targeting a configured catalog name or OCI registry URL.

---

## Configuration (`config` and `catalog remote`)

`scafctl` follows the XDG Base Directory Specification for its configuration file (`~/.config/scafctl/config.yaml` on Linux/macOS, `%LOCALAPPDATA%\scafctl\config.yaml` on Windows) managed via [Viper](https://github.com/spf13/viper). Every setting can be overridden via `SCAFCTL_`-prefixed environment variables.

### Config File Structure

~~~yaml
version: 1
catalogs:
  - name: local
    type: filesystem
    path: ~/.local/share/scafctl/catalog
  - name: internal
    type: oci
    url: oci://ghcr.io/my-org
    authProvider: github
settings:
  defaultCatalog: internal
action:
  outputDir: "/path/to/output"
~~~

### Config Commands

~~~bash
# Initialize a starter or full reference config file
scafctl config init
scafctl config init --full

# Inspect, query, modify, validate, or reset configuration
scafctl config view
scafctl config paths
scafctl config schema
scafctl config validate
scafctl config get settings.defaultCatalog
scafctl config set settings.defaultCatalog internal
scafctl config unset settings.defaultCatalog
scafctl config reset --force
~~~

### Remote Catalog Management (`catalog remote`)

Remote catalog entries in `config.yaml` are managed under `scafctl catalog remote` (not `scafctl config`):

~~~bash
# Add an OCI catalog (scafctl appends /solutions/<name>, /providers/<name>, etc. to --url)
scafctl catalog remote add internal --type oci --url oci://ghcr.io/my-org --auth-provider github
scafctl catalog remote add org-solutions --type oci --url oci://ghcr.io/my-org --artifact-kind solution

# List configured catalogs
scafctl catalog remote list

# Set or clear the default catalog
scafctl catalog remote default internal

# Remove a catalog
scafctl catalog remote remove internal
~~~

### Environment Variables

~~~bash
export SCAFCTL_SETTINGS_DEFAULTCATALOG=internal
export SCAFCTL_CONFIG=/path/to/custom/config.yaml
~~~

---

## Managing Secrets, State, Cache, and Plugins

### Encrypted Secrets (`secrets`)

Securely manage AES-256-GCM encrypted secrets backed by the OS keyring (`~/.local/share/scafctl/secrets/` on Linux/macOS, `%LOCALAPPDATA%\scafctl\secrets\` on Windows):

~~~bash
# List, get, set, check, and delete secrets (--all includes internal scafctl.* tokens)
scafctl secrets list
scafctl secrets list --all
scafctl secrets get my-api-key
echo -n "secret-value" | scafctl secrets set my-api-key
scafctl secrets set my-api-key --value="secret-value"
scafctl secrets exists my-api-key
scafctl secrets delete my-api-key

# Export secrets (pass --encrypt for password-protected AES-256-GCM; -o selects yaml or json)
scafctl secrets export --output-file secrets.enc --encrypt
scafctl secrets export --output-file secrets.json -o json --encrypt

# Import secrets from a positional file path (format auto-detected)
scafctl secrets import secrets.enc
scafctl secrets import secrets.enc --dry-run

# Rotate the master encryption key
scafctl secrets rotate
~~~

### Solution State (`state`), Cache (`cache`), and Plugins (`plugins`)

~~~bash
# Inspect and manage persisted solution state files
scafctl state list -f state.yaml
scafctl state show -f state.yaml
scafctl state get --key region -f state.yaml
scafctl state set --key region --value us-east1 -f state.yaml
scafctl state fingerprints -f state.yaml
scafctl state delete --key region -f state.yaml
scafctl state clear -f state.yaml

# View or clear HTTP, build, and artifact caches
scafctl cache info
scafctl cache clear --kind artifact --force

# Pre-fetch, list, update, or prune cached plugin binaries
scafctl plugins install -f ./solution.yaml
scafctl plugins list
scafctl plugins update --all
scafctl plugins prune
~~~

---

## Authentication, Kubernetes, and Credential Helpers

Manage authentication sessions, profiles, and token bridging across cloud and OIDC providers:

~~~bash
# Authenticate and bridge credentials to an OCI registry
scafctl auth login github --registry ghcr.io
scafctl auth login entra

# Inspect status, cached tokens, handlers, and diagnostics
scafctl auth status
scafctl auth list
scafctl auth handlers
scafctl auth diagnose

# Manage named auth profiles and retrieve debug tokens
scafctl auth switch entra
scafctl auth switch entra personal
scafctl auth profile delete entra personal
scafctl auth token entra --scope "https://graph.microsoft.com/.default"
scafctl auth logout entra

# Wire kubectl/oc exec-credential authentication or Docker/Podman credential helpers
scafctl kube login cluster-a --handler openshift
scafctl credential-helper install --docker
~~~

**Supported auth handlers**:

- **Official first-party handlers** (auto-fetched on demand from the official catalog):
  - `entra` -- Microsoft Entra ID (formerly Azure AD)
  - `github` -- GitHub and GitHub Enterprise Server
  - `gcp` -- Google Cloud Platform
  - `openshift` -- Red Hat OpenShift OAuth
- **Custom OAuth2 handlers**: Configured under `auth.customOAuth2` (`scafctl auth oauth2`)
- **Plugin auth handlers**: Third-party or organization-internal gRPC auth-handler plugins distributed via OCI catalogs

---

## Resolver Commands (`run resolver`)

> [!NOTE]
> The standalone `scafctl resolver graph` command has been removed. Use `scafctl run resolver --graph` instead.

The `run resolver` command executes resolvers without running workflow actions:

~~~bash
# Run all resolvers or selected resolvers (with transitive dependencies)
scafctl run resolver -f solution.yaml
scafctl run resolver db config -f solution.yaml

# Run resolvers from a catalog solution (-s/--solution) or scope to an action (-a/--action)
scafctl run resolver -s my-app@1.0.0 env=prod
scafctl run resolver -f solution.yaml -a deploy

# Structured JSON output (values only vs. including __execution timing/phase metadata)
scafctl run resolver -f solution.yaml -o json
scafctl run resolver --show-execution -f solution.yaml -o json

# Skip transform and validation phases
scafctl run resolver --skip-transform -f solution.yaml

# Dependency graph (ascii, dot, mermaid, json)
scafctl run resolver --graph -f solution.yaml
scafctl run resolver --graph --graph-format=dot -f solution.yaml

# Snapshot execution state
scafctl run resolver --snapshot --snapshot-file=out.json --redact -f solution.yaml

# Interactive TUI
scafctl run resolver -f solution.yaml -i
~~~

Aliases: `res`, `resolvers`

---

## Evaluating Expressions (`eval`)

The `eval` command group tests CEL expressions and Go templates in isolation before embedding them in solutions.

### Evaluate CEL (`eval cel`)

~~~bash
# Simple expression (--expression is required)
scafctl eval cel --expression "1 + 2"

# With inline variables (-v/--var) or JSON data (-d/--data)
scafctl eval cel --expression 'size(name) > 3' -v name=hello
scafctl eval cel --expression '_.name == "test"' -d '{"name": "test"}'

# With a JSON or YAML data file (-f/--file, bound to '_')
scafctl eval cel --expression '_.items.size() > 0' -f data.json
scafctl eval cel --expression '_.items.filter(i, i.active)' -f data.json -o json
~~~

### Evaluate Go Template (`eval template`)

~~~bash
# Inline template (-t/--template) with inline JSON data (-d/--data)
scafctl eval template -t '{{.name}}' -d '{"name": "hello"}'

# Template file (--template-file) with data file (-f/--file) and reference extraction
scafctl eval template --template-file template.txt -f data.json
scafctl eval template --template-file template.txt -f data.json --show-refs

# Redirect rendered output to a file
scafctl eval template --template-file template.txt -f data.json > result.txt
~~~

### Validate Expression Syntax (`eval validate`) and Extract References (`eval refs`)

~~~bash
# Check CEL or Go template syntax without evaluating
scafctl eval validate --expression 'size(_.items) > 0' --type cel
scafctl eval validate --expression '{{ .name }}' --type go-template

# Extract _.name resolver references used by an expression or template
scafctl eval refs --expr '_.config.host + ":" + string(_.config.port)'
scafctl eval refs --template '{{ ._.config.host }}:{{ ._.config.port }}'
~~~

> [!NOTE]
> To validate an entire solution YAML file, use `scafctl validate -f solution.yaml` (or `scafctl lint -f solution.yaml`).

---

## Creating New Solutions (`new solution`)

Scaffold a new solution YAML from built-in templates (`-d`/`--description` is required; output defaults to stdout unless `--output-file` is given):

~~~bash
# Scaffold using positional name and write to a file
scafctl new solution my-solution -d "My new solution" --output-file my-solution.yaml

# Include specific features and provider examples
scafctl new solution my-solution -d "Deploy service" \
  --features parameters,resolvers,actions,tests \
  --providers file,go-template,cel \
  --version 1.0.0 \
  --output-file solution.yaml
~~~

---

## Validation Gate (`validate`) and Functional Testing (`test`)

`validate` is the primary pass/fail gate for CI pipelines and pre-commit hooks. `lint` is the advisory subset that reports authoring findings and applies `--fix` rewrites.

### Validate Solutions, Resolvers, and Schemas

~~~bash
# Validate auto-discovered solution, local file, catalog ref, or stdin
scafctl validate
scafctl validate solution -f ./my-solution.yaml
cat ./my-solution.yaml | scafctl validate -f -

# Treat lint warnings as fatal
scafctl validate solution -f ./my-solution.yaml --strict

# Execute resolver phases + lint as a strict CI gate
scafctl validate resolver -f ./my-solution.yaml --strict
scafctl validate resolver -s my-app@1.0.0 -a deploy --strict

# Validate arbitrary JSON/YAML data against a JSON Schema
scafctl validate schema --schema schema.json --data data.json
cat data.yaml | scafctl validate schema --schema schema.json --data -
~~~

### Functional Testing (`test` / `test functional`)

Execute sandboxed functional test cases declared under `spec.testing.cases` (invoking `scafctl test` with no subcommand runs `test functional` with identical flags):

~~~bash
# Run all tests (including builtin parse, lint, and resolve-defaults checks)
scafctl test
scafctl test -f ./solution.yaml
scafctl test functional my-solution@1.0.0

# Filter tests, update golden snapshots, or emit JUnit XML
scafctl test -f ./solution.yaml --filter "render-*" --tag smoke
scafctl test -f ./solution.yaml --update-snapshots
scafctl test -f ./solution.yaml --report-file results.xml

# List test cases or scaffold a starter test suite
scafctl test list -f ./solution.yaml
scafctl test init -f ./solution.yaml
~~~

---

## Refactoring Solutions (`refactor`)

`refactor` applies source-preserving, byte-exact edits to a solution without a YAML round-trip, keeping comments, key order, and formatting intact:

~~~bash
# Preview occurrences without writing
scafctl refactor rename resolver environment env -f ./solution.yaml --dry-run

# Rename a resolver, workflow action, reusable call, or author-defined function in place
scafctl refactor rename resolver environment env
scafctl refactor rename action deploy release
scafctl refactor rename call fetch download
scafctl refactor rename function greet salute
~~~

Exit codes: `0` applied (or previewed), `2` validation error (invalid name, collision, undefined symbol, or unlocatable reference), `4` file not found or parse failure.

---

## Server and Editor Integration (`mcp`, `lsp`, `serve`)

### MCP Server (`mcp init`, `mcp serve`, `mcp list`)

Expose `scafctl` schema, lint, test, dry-run, and packaging tools to AI coding assistants via Model Context Protocol:

~~~bash
# Auto-detect workspace AI harnesses (vscode, claude, cursor, opencode, agy) and install MCP + SKILL.md
scafctl mcp init
scafctl mcp init --target vscode --target claude
scafctl mcp init --dry-run

# Start the MCP server (stdio default, or sse/http)
scafctl mcp serve
scafctl mcp serve --transport http --addr 127.0.0.1:8080
scafctl mcp serve --info

# Inspect registered MCP tools and prompts
scafctl mcp list
scafctl mcp list --kind tool -o json
~~~

### Language Server (`lsp`)

`scafctl lsp` runs a Language Server Protocol server over stdio for real-time editor diagnostics, hover docs, completion, signature help, go-to-definition, find-references, code actions, and rename:

~~~bash
scafctl lsp
scafctl lsp --stdio
scafctl lsp document-selectors -o json
~~~

### REST API Server (`serve` and `serve openapi`)

~~~bash
# Start the REST API server (chi + Huma OpenAPI)
scafctl serve
scafctl serve --port 9090

# Export the OpenAPI specification without starting the server
scafctl serve openapi -o yaml --output-file openapi.yaml
~~~

---

## Auto-fixing and Exploring Lint Rules (`lint`)

~~~bash
# Preview or apply auto-fixable lint rewrites (e.g. hyphenated-name -> camelCase)
scafctl lint --fix-dry-run --diff -f ./solution.yaml
scafctl lint --fix -f ./solution.yaml

# List all lint rules or explain a single rule in detail
scafctl lint rules
scafctl lint rule no-default-action
scafctl lint rule builtin-in-bundle-plugins -o json
~~~

> [!NOTE]
> The former `scafctl lint explain <rule-id>` remains available only as a hidden deprecated alias for `scafctl lint rule <rule-id>`.

---

## Browsing Examples (`get examples`)

Built-in `kind: Solution` examples are embedded directly in the binary and queried via `scafctl get examples`:

~~~bash
# List all embedded examples or filter by category
scafctl get examples
scafctl get examples --category solutions
scafctl get examples -i

# View an example by exact embedded path, metadata.name, slug, or unique substring
scafctl get examples resolvers/hello-world.yaml
scafctl get examples cel-basics
scafctl get examples state-fingerprint > output.yaml
~~~

---

## Help and Discovery

~~~bash
scafctl --help
scafctl options
scafctl run --help
scafctl run solution --help
~~~

---

## Command Grammar: Verb-Noun vs Noun-Verb

### Current State of `scafctl`

`scafctl` uses two distinct command grammar patterns plus standalone server/gate entrypoints:

**Verb-Noun** (`kubectl`-style) -- the verb is the top-level command, the domain kind follows:

| Command | Verb | Noun |
|---------|------|------|
| `scafctl run solution` | run | solution |
| `scafctl run action` | run | action |
| `scafctl run resolver` | run | resolver |
| `scafctl run provider` | run | provider |
| `scafctl render solution` | render | solution |
| `scafctl inspect solution` | inspect | solution |
| `scafctl get solution` | get | solution |
| `scafctl get provider` | get | provider |
| `scafctl get examples` | get | examples |
| `scafctl get snapshot` | get | snapshot |
| `scafctl explain <kind>` | explain | `<kind>` |
| `scafctl package solution` | package | solution |
| `scafctl package plugin` | package | plugin |
| `scafctl extract bundle` | extract | bundle |
| `scafctl diff solution` | diff | solution |
| `scafctl diff bundle` | diff | bundle |
| `scafctl diff snapshot` | diff | snapshot |
| `scafctl validate solution` | validate | solution |
| `scafctl validate resolver` | validate | resolver |
| `scafctl validate schema` | validate | schema |
| `scafctl new solution` | new | solution |

**Noun-Verb** -- the subsystem noun is the top-level command, the action verb follows:

| Command | Noun | Verb |
|---------|------|------|
| `scafctl catalog list` | catalog | list |
| `scafctl catalog inspect` | catalog | inspect |
| `scafctl catalog push` | catalog | push |
| `scafctl catalog pull` | catalog | pull |
| `scafctl catalog tags` | catalog | tags |
| `scafctl catalog remote` | catalog | remote (`add`, `remove`, `default`, `list`) |
| `scafctl vendor update` | vendor | update |
| `scafctl config init` | config | init |
| `scafctl config view` | config | view |
| `scafctl config set` | config | set |
| `scafctl secrets get` | secrets | get |
| `scafctl secrets set` | secrets | set |
| `scafctl secrets export` | secrets | export |
| `scafctl state list` | state | list |
| `scafctl auth login` | auth | login |
| `scafctl auth status` | auth | status |
| `scafctl kube login` | kube | login |
| `scafctl cache info` | cache | info |
| `scafctl cache clear` | cache | clear |
| `scafctl plugins install` | plugins | install |
| `scafctl plugins list` | plugins | list |
| `scafctl mcp init` | mcp | init |
| `scafctl mcp serve` | mcp | serve |
| `scafctl mcp list` | mcp | list |
| `scafctl lint rules` | lint | rules |
| `scafctl lint rule` | lint | rule |
| `scafctl eval cel` | eval | cel |
| `scafctl eval template` | eval | template |
| `scafctl refactor rename` | refactor | rename |
| `scafctl serve openapi` | serve | openapi |

**Standalone / Default-Subcommand Entrypoints**:

| Command | Notes |
|---------|-------|
| `scafctl validate` | Runs `validate solution` when no subcommand is given |
| `scafctl lint` | Runs `lint solution` when no subcommand is given |
| `scafctl test` | Runs `test functional` when no subcommand is given |
| `scafctl serve` | Starts the REST API server |
| `scafctl lsp` | Starts the LSP server over stdio |
| `scafctl version` | Prints version metadata |
| `scafctl options` | Lists global flags |

---

### What Major CLIs Do

Most widely adopted developer CLIs converge on the same hybrid pattern `scafctl` uses:

| CLI | Core Domain Objects | Service/Infrastructure | Example |
|-----|---------------------|------------------------|---------|
| **kubectl** | verb-noun: `get pods`, `delete svc`, `apply -f` | noun-verb: `config use-context`, `auth can-i` | Domain is verb-noun; plumbing is noun-verb |
| **docker** | verb-noun: `run`, `build`, `pull`, `push` | noun-verb: `network create`, `volume ls`, `system prune` | Top-level verbs act on images/containers; subsystems are noun-verb |
| **git** | verb-first: `clone`, `commit`, `push`, `pull` | noun-verb: `remote add`, `branch delete`, `stash pop` | Core workflow is verbs; ancillary resource management is noun-verb |
| **gh** (GitHub CLI) | verb-noun: `pr create`, `issue list` | noun-verb: `auth login`, `config set`, `secret set` | Domain objects verb-noun; infrastructure noun-verb |
| **az** (Azure CLI) | noun-verb: `az vm create`, `az storage blob upload` | noun-verb throughout | Purely noun-verb (resource-group style) |
| **gcloud** | noun-verb: `gcloud compute instances create` | noun-verb throughout | Purely noun-verb (resource hierarchy) |
| **terraform** | verb-first: `plan`, `apply`, `destroy` | -- | No sub-resources, single verb layer |
| **helm** | verb-noun: `install`, `upgrade`, `rollback` | noun-verb: `repo add`, `plugin install` | Charts are verb-noun; supporting systems noun-verb |

---

### Best Practice: The Delineation Rule

The hybrid pattern follows a clear principle:

> **Use verb-noun for domain operations on core business objects.
> Use noun-verb for infrastructure, plumbing, and service management.**

| Category | Pattern | Rationale | `scafctl` examples |
|----------|---------|-----------|--------------------|
| **Core domain operations** | `<verb> <kind>` | The user thinks in terms of *what they want to do*: run, get, render, inspect, package, diff, validate. | `run solution`, `run action`, `get provider`, `render solution`, `package solution`, `new solution` |
| **Infrastructure / services** | `<noun> <action>` | The user thinks in terms of *which subsystem* they need to manage. | `catalog push`, `catalog pull`, `catalog remote`, `vendor update`, `config set`, `secrets get`, `auth login`, `mcp init` |
| **Standalone / default entrypoints** | `<verb>` or `<noun>` | Single-purpose servers, info commands, or top-level gates with a default subcommand. | `validate`, `lint`, `test`, `serve`, `lsp`, `version`, `options` |

### Verdict on `scafctl`

`scafctl` follows the established delineation consistently:

- **Verb-noun** for core domain operations -- `run`, `render`, `inspect`, `get`, `explain`, `package`, `extract`, `diff`, `validate`, and `new` act on domain kinds (`solution`, `action`, `resolver`, `provider`, `bundle`, `snapshot`, `schema`).
- **Noun-verb** for infrastructure and subsystems -- `catalog` (`list`, `inspect`, `push`, `pull`, `tags`, `remote`, `attach`, `index`), `vendor` (`update`), `config`, `secrets`, `state`, `auth`, `kube`, `cache`, `credential-helper`, `plugins`, `mcp` (`init`, `serve`, `list`), `lint`, `eval`, and `refactor` group related subsystem operations cleanly.
- **Standalone / default-subcommand entrypoints** -- `validate`, `lint`, `test`, `serve`, `lsp`, `version`, and `options`.

When adding a new command, ask *"Is the user performing a core domain action on a kind, or managing a subsystem?"* -- use verb-noun for the former and noun-verb for the latter.

---
title: "CLI"
weight: 8
---

# CLI Usage

This document describes how to invoke scafctl from the command line. The CLI follows a kubectl-style structure where verbs, kinds, and names are explicit and positional.

The general pattern is:

~~~text
scafctl <verb> <kind> <name[@version(or constraint)]> [flags]
~~~

- `<verb>` describes what you want to do
- `<kind>` identifies the type of object
- `<name>` identifies the object
- `@version` is optional and resolved via the catalog ( or constraint)

---

## Implementation Status

| Command | Status | Notes |
|---------|--------|-------|
| `run solution` | ✅ Implemented | Requires workflow (errors if no workflow defined; use `run resolver` for resolver-only) |
| `run resolver` | ✅ Implemented | Resolver-only execution for debugging and inspection |
| `render solution` | ✅ Implemented | Includes action-graph and snapshot modes |
| `get solution/provider/resolver` | ✅ Implemented | |
| `explain <kind>` | ✅ Implemented | Schema docs for resource kinds (provider, action, resolver) |
| `inspect solution [--usage]` | ✅ Implemented | Instance structure; `--usage` for the user-facing usage view |
| `config *` | ✅ Implemented | view, get, set, unset, add-catalog, remove-catalog, use-catalog, init, schema, validate |
| `get snapshot` | ✅ Implemented | |
| `diff solution` | ✅ Implemented | Structural comparison of two solution files |
| `secrets *` | ✅ Implemented | list, get, set, delete, exists, export, import, rotate |
| `auth *` | ✅ Implemented | login, logout, status, token |
| `resolver graph` | ❌ Removed | Use `run resolver --graph` instead |
| `package solution` | ✅ Implemented | Catalog feature |
| `catalog list/inspect/delete/prune` | ✅ Implemented | Catalog management |
| `catalog save/load` | ✅ Implemented | Offline distribution |
| `eval cel` | ✅ Implemented | Evaluate CEL expressions from CLI |
| `eval template` | ✅ Implemented | Evaluate Go templates from CLI |
| `eval validate` | ✅ Implemented | Validate solution files from CLI |
| `validate solution` | ✅ Implemented | Primary gate: loads a solution and runs lint (errors fail, warnings surface, `--strict` makes warnings fatal) |
| `validate resolver` | ✅ Implemented | Validates resolvers, then runs lint as a gate (`--strict` makes lint warnings fatal) |
| `validate schema` | ✅ Implemented | Validate arbitrary data (JSON/YAML, `--data -` for stdin) against a JSON Schema |
| `new solution` | ✅ Implemented | Scaffold a new solution from template |
| `lint rules` | ✅ Implemented | List all available lint rules |
| `lint rule` | ✅ Implemented | Explain a specific lint rule |
| `lint` | ✅ Implemented | Advisory subset of `validate`: reports authoring warnings/findings without being the pass/fail gate |
| `examples list` | ✅ Implemented | List available example configurations |
| `examples get` | ✅ Implemented | Get/download an example file |
| `push solution/plugin` | 📋 Planned | Remote catalog feature |
| `pull solution/plugin` | 📋 Planned | Remote catalog feature |
| `tag solution/plugin` | 📋 Planned | Catalog feature |
| `--catalog` flag | 📋 Planned | Catalog feature |
| Version constraints (`@^1.2`) | 📋 Planned | Requires catalog |

---

## Core Concepts

### Kinds

- `solution`
- `provider`
- `resolver`
- `catalog` *(planned)*

---

### Names and Versions

Names identify an object within a kind.

Versions are optional and may be:

- an exact version (`1.0.0`)
- a constraint (`^1.2`, `>=1.0 <2.0`) *(planned - requires catalog)*
- omitted (default resolution rules apply)

**Shell escaping**: Complex version constraints with special characters should be quoted:

~~~bash
scafctl run solution "example@>=1.0 <2.0"  # planned
scafctl run solution "example@^1.2"         # planned
~~~

### File Paths vs. Catalog References

When a command accepts both a positional name argument and a `-f`/`--file` flag, the CLI distinguishes catalog references from local file paths:

| Input | Interpretation | Example |
|-------|---------------|---------|
| Bare name | Catalog reference | `my-app`, `deploy` |
| Versioned name | Catalog reference | `my-app@1.0.0`, `deploy@^1.2` |
| Registry reference | Catalog reference | `ghcr.io/org/sol:v1`, `localhost:5000/sol` |
| URL | Remote reference | `https://example.com/sol.yaml` |
| Starts with `/` or `.` | **Rejected** — use `-f` | `/tmp/sol.yaml`, `./sol.yaml` |
| Ends with `.yaml`, `.yml`, `.json` | **Rejected** — use `-f` | `solution.yaml` |
| Path with separators, non-hostname first segment | **Rejected** — use `-f` | `configs/solution`, `relative/path/sol` |
| Windows path | **Rejected** — use `-f` | `C:\dir\sol`, `dir\sol` |

To pass a local file path, always use the `-f`/`--file` flag:

~~~bash
# Correct: file via -f flag
scafctl run solution -f ./solution.yaml

# Correct: catalog reference as positional arg
scafctl run solution my-app@1.0.0

# Error: local file path as positional arg
scafctl run solution solution.yaml   # rejected with helpful error
~~~

This separation keeps the CLI unambiguous — positional arguments are always catalog/registry lookups while `-f` is always a file path.

---

## Running a Solution

Execute a solution's resolvers and perform its workflow actions. The solution **must** define a `workflow` section with actions — if no workflow is defined, the command errors and suggests using `scafctl run resolver` instead.

~~~bash
scafctl run solution example
~~~

Run a specific version:

~~~bash
scafctl run solution example@1.0.0
~~~

Run with a version constraint:

~~~bash
scafctl run solution example@^1.2
~~~

---

## Getting a Solution

Show metadata of the latest example solution:

~~~bash
scafctl get solution example
~~~

Show metadata of version 1.0.0 of the example solution:

~~~bash
scafctl get solution example@1.0.0
~~~

### Listing Resources

Following kubectl conventions, use singular or plural forms:

~~~bash
# List all solutions in the catalog
scafctl get solutions

# List all providers (built-in + official)
scafctl get providers

# Get a specific solution
scafctl get solution example
~~~

Both singular and plural forms without a name will list all resources of that kind.

The `get providers` output includes a `source` column indicating whether each provider is `builtin` (compiled in) or `official` (auto-fetched from the OCI catalog). When filters (`--capability`, `--category`) are active, only built-in providers are shown since official providers do not expose capability/category metadata
---

## Rendering a Solution

Render executes resolvers and renders actions but does not perform side effects.

### From Catalog (by name)

~~~bash
scafctl render solution example
~~~

### From File

Use `-f` or `--file` to specify a file path:

~~~bash
scafctl render solution -f mysolution.yaml
~~~

From stdin:

~~~bash
cat solution1.yaml | scafctl render solution -f -
~~~

**Note**: The `-f` flag is used consistently across commands (`run`, `render`, `publish`) to indicate a file source rather than a catalog lookup.

Render a specific version:

~~~bash
scafctl render solution example@1.0.0
~~~

Typical uses:

- dry runs
- snapshot testing
- delegating execution to another system
- reviewing generated artifacts

---

## Passing Resolver Parameters

Resolver parameters are passed using `-r` or `--resolver`.

~~~bash
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

Resolver parameters (and other similar flags) use a `key=value` format where:

- **Key**: Simple string identifier (no spaces or newlines allowed)
- **Value**: Supports ALL characters including newlines, special characters, quotes, etc.

#### Basic Usage

The flag can be repeated for each key-value pair:

~~~bash
scafctl run solution example \
  -r someKey=sk_live_abc123 \
  -r config='{"nested": "json"}' \
  -r script="line1
line2
line3"
~~~

#### CSV Support

**New**: You can also pass multiple comma-separated `key=value` pairs in a single flag:

~~~bash
# Multiple pairs in one flag
scafctl run solution example \
  -r "env=prod,region=us-east1,region=us-west1"
~~~

To include commas in values, use quotes:

~~~bash
# Quoted values preserve commas as literal characters
scafctl run solution example \
  -r "msg=\"Hello, world\"" \
  -r "data='item1,item2,item3'"
~~~

Escaped quotes are supported within quoted values:

~~~bash
scafctl run solution example \
  -r "json=\"{\\\"key\\\":\\\"value\\\"}\""
~~~

#### Multiple Values for Same Key

Values for the same key are automatically combined, whether using CSV or repeated flags:

~~~bash
# Using repeated flags
scafctl run solution example \
  -r region=us-east1 \
  -r region=us-west1 \
  -r region=eu-west1

# Using CSV in one flag
scafctl run solution example \
  -r "region=us-east1,region=us-west1,region=eu-west1"

# Combining both approaches
scafctl run solution example \
  -r "region=us-east1,region=us-west1" \
  -r region=eu-west1

# All three produce: region = [us-east1, us-west1, eu-west1]
~~~

#### Usage Patterns

**Separate flags (traditional):**

~~~bash
scafctl run solution example \
  -r key1=value1 \
  -r key2=value2
~~~

**CSV in single flag (convenient for multiple pairs):**

~~~bash
scafctl run solution example \
  -r "key1=value1,key2=value2"
~~~

**Mixed approach:**

~~~bash
scafctl run solution example \
  -r "env=prod,region=us-east1" \
  -r region=us-west1 \
  -r apiKey=secret
~~~

**Technical Note**: The CLI uses `StringArrayVarP` with custom CSV parsing (via `pkg/flags.ParseKeyValueCSV`) to avoid Cobra's built-in CSV issues while still supporting comma-separated values with proper quote handling

#### URI Scheme Support

To simplify passing complex data like JSON or YAML without escaping, use URI scheme prefixes:

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

**Important**: The scheme prefix is preserved and should be processed by your solution logic.

**Validation**: Values with URI schemes are automatically validated:

- `json://` - Validated as well-formed JSON
- `yaml://` - Validated as well-formed YAML
- `base64://` - Validated as proper base64 encoding
- `file://` - Verified that file exists and is not a directory
- `http://`, `https://` - Validated as properly formatted URLs

Validation errors are reported immediately with helpful messages.

#### Parameter File References (@file)

Load all parameters from a YAML or JSON file using the `@` prefix:

~~~bash
# Load from a YAML file
scafctl run resolver -f solution.yaml -r @params.yaml

# Load from a JSON file
scafctl run resolver -f solution.yaml -r @params.json

# Mix file and inline parameters
scafctl run resolver -f solution.yaml -r @defaults.yaml -r env=prod
~~~

#### Stdin Parameters (@-)

Read all parameters from stdin as YAML or JSON using `@-`, following the same convention as curl:

~~~bash
# Pipe JSON from echo
echo '{"env": "prod", "region": "us-east1"}' | scafctl run resolver -f solution.yaml -r @-

# Pipe from a file via cat
cat params.yaml | scafctl run solution example -r @-

# Use as positional argument
echo '{"env": "prod"}' | scafctl run resolver -f solution.yaml @-
~~~

#### Per-Key Stdin and File References (key=@-  /  key=@file)

Assign raw content from stdin or a file directly to a single key. Unlike standalone `@-` (which parses YAML/JSON into multiple keys), `key=@-` reads stdin as a raw string value:

~~~bash
# Pipe raw text into a single parameter
echo hello | scafctl run provider message message=@-
echo hello | scafctl run resolver -f solution.yaml -r message=@-

# Read a file's raw content into a parameter
scafctl run provider http url=https://example.com body=@request.json
scafctl run resolver -f solution.yaml -r config=@defaults.txt

# Mix with other parameter forms
scafctl run resolver -f solution.yaml -r name=Alice body=@template.txt
~~~

> A single trailing newline is trimmed automatically (matching shell `echo` behavior).

**Restrictions:**

- `@-` can only appear once (stdin is consumed on first read) — this applies across both standalone `@-` and `key=@-`
- `@-` cannot be combined with `-f -` (both read from stdin)

---

## Rendering With Parameters

~~~bash
scafctl render solution example \
  -r env=staging \
  -r dryRun=true
~~~

### Render Options

The `render` command supports additional modes for debugging and testing:

#### Execution Snapshots

Capture resolver execution state for testing and comparison:

~~~bash
# Save snapshot after rendering
scafctl render solution -f solution.yaml --snapshot output.json

# Redact sensitive values
scafctl render solution -f solution.yaml --snapshot output.json --redact
~~~

Snapshots can be analyzed with dedicated commands:

~~~bash
# Display a saved snapshot
scafctl get snapshot output.json

# Compare two snapshots
scafctl diff snapshot before.json after.json
~~~

---

## Working With the Catalog

> **Status**: ✅ Implemented - Local catalog with build, list, inspect, delete, prune, save, and load.
> Remote push/pull planned for Phase 2.

Run a solution directly from the catalog:

~~~bash
scafctl run solution example@1.7.0
~~~

### Building Artifacts

> **Status**: ✅ Implemented

Package a solution into the local catalog (analogous to `docker build`):

> Note: `build` is a backward-compatible alias for `package` (e.g. `scafctl build solution` still works).

~~~bash
# Package a solution from file
scafctl package solution -f ./solution.yaml --version 1.0.0

# Package using version from metadata
scafctl package solution -f ./solution.yaml

# Overwrite existing version
scafctl package solution -f ./solution.yaml --version 1.0.0 --force
~~~

The build process validates, resolves dependencies, bundles local files, vendors catalog dependencies, and packages artifacts into the local catalog. See [catalog-build-bundling.md](../design/catalog-build-bundling.md) for the full bundling design.

Additional build flags:

~~~bash
# Dry-run: show what would be bundled without building
scafctl package solution -f ./solution.yaml --dry-run

# Skip file bundling (legacy single-layer artifact)
scafctl package solution -f ./solution.yaml --no-bundle

# Skip vendoring catalog dependencies
scafctl package solution -f ./solution.yaml --no-vendor

# Set max bundle size
scafctl package solution -f ./solution.yaml --bundle-max-size 100MB

# Re-resolve and update the lock file
scafctl package solution -f ./solution.yaml --update-lock
~~~

### Publishing Artifacts

> **Status**: 📋 Planned

Push artifacts to a remote catalog (analogous to `docker push`):

~~~bash
# Push a solution
scafctl push solution my-solution@1.7.0

# Push a plugin
scafctl push plugin aws-provider@1.5.0

# Push to a specific catalog
scafctl push solution my-solution@1.7.0 --catalog=production
~~~

### Pulling Artifacts

> **Status**: 📋 Planned

Pull artifacts from a remote catalog to local (analogous to `docker pull`):

~~~bash
# Pull a solution
scafctl pull solution example@1.7.0

# Pull a plugin
scafctl pull plugin aws-provider@1.5.0
~~~

### Inspecting Artifacts

> **Status**: ✅ Implemented

View artifact metadata, dependencies, and structure:

~~~bash
# Inspect a solution (latest version)
scafctl catalog inspect example

# Inspect specific version
scafctl catalog inspect example@1.7.0

# JSON output
scafctl catalog inspect example -o json
~~~

### Tagging Artifacts

> **Status**: 📋 Planned

Create version aliases:

~~~bash
# Tag a solution
scafctl tag solution my-solution@1.2.3 my-solution:latest

# Tag a plugin
scafctl tag plugin aws-provider@1.5.0 aws-provider:stable
~~~

### Offline Distribution

> **Status**: ✅ Implemented

Export and import artifacts for air-gapped environments (analogous to `docker save/load`):

~~~bash
# Save a solution (exports latest version by default)
scafctl catalog save my-solution -o solution.tar

# Save specific version
scafctl catalog save my-solution@1.2.3 -o solution.tar

# Load from archive
scafctl catalog load --input solution.tar

# Force overwrite if artifact already exists
scafctl catalog load --input solution.tar --force
~~~

The archive uses OCI Image Layout format, making it compatible with OCI registry tools.

### Deleting Artifacts

> **Status**: ✅ Implemented

Remove an artifact from the local catalog:

~~~bash
# Delete specific version (version required)
scafctl catalog delete example@1.7.0

# Prune orphaned blobs after deletion
scafctl catalog prune
~~~

### Catalog Resolution

> **Status**: 📋 Planned

By default, scafctl uses the local filesystem as the default catalog. Use `--catalog` to target a specific configured catalog:

~~~bash
scafctl run solution example --catalog=internal
scafctl get solutions --catalog=production
~~~

---

## Inspecting and Explaining Resources

scafctl separates schema documentation from instance inspection (see
`docs/design/information-command-taxonomy.md`):

- `explain <kind>` -- schema documentation for a resource *kind* (from struct
  tags), e.g. `explain provider`, `explain action`, `explain resolver`. Drill
  into fields with `explain provider.schema`.
- `inspect <resource>` -- structure and metadata of a specific *instance*
  (kvx-native), e.g. `inspect solution`.

### Inspect Solution (structure)

~~~bash
# From file
scafctl inspect solution -f solution.yaml

# From catalog
scafctl inspect solution example
scafctl inspect solution example@1.0.0

# Structured output
scafctl inspect solution -f solution.yaml -o json
~~~

Outputs the solution's resolvers, actions, parameters, file dependencies, and
the inferred run command.

### Inspect Solution (usage view)

Use `--usage` for the user-facing "how do I run this?" view -- a synopsis, the
user-supplied parameters (with types, defaults, and discovered allowed values),
and each runnable action with the exact command to invoke it:

~~~bash
scafctl inspect solution -f solution.yaml --usage

# Structured output for tooling
scafctl inspect solution -f solution.yaml --usage -o json
~~~

Authors can enrich the view with an optional `metadata.usage` block:

- `synopsis` -- a one-line usage summary (overrides the metadata description)
- `details` -- long-form prose about how the solution works and when to use it
- `examples` -- curated `description`/`command` pairs

Along with the auto-projected `displayName`, `tags`, `links`, per-parameter
`description`/`example`, and per-action `description`. When `metadata.usage` is
absent, the view is generated entirely from parameters and actions.

### Explain Schema Kinds

~~~bash
# Provider descriptor schema
scafctl explain provider

# Drill into a field
scafctl explain provider.schema

# Action / resolver schema
scafctl explain action
scafctl explain resolver
~~~

Outputs the struct definition, field types, validation rules, and documentation
extracted from Go struct tags.

---

## Global Flags

These flags are available on all commands. Run `scafctl options` to see them:

| Flag | Short | Description | Status |
|------|-------|-------------|--------|
| `--cwd` | `-C` | Change the working directory before executing the command (similar to `git -C`) | ✅ Implemented |
| `--quiet` | `-q` | Suppress non-essential output | ✅ Implemented |
| `--no-color` | | Disable colored output | ✅ Implemented |
| `--config` | | Path to config file (default: `~/.scafctl/config.yaml`) | ✅ Implemented |
| `--log-level` | | Set log level (none, error, warn, info, debug, trace, or numeric V-level) | ✅ Implemented |
| `--debug` | `-d` | Enable debug logging (shorthand for --log-level debug) | ✅ Implemented |
| `--log-format` | | Log format: console (default) or json | ✅ Implemented |
| `--log-file` | | Write logs to a file path | ✅ Implemented |
| `--catalog` | | Target a specific configured catalog | 📋 Planned |

**Note**: The `-o/--output` flag is available per-command (not global) on commands that support structured output.

**Output format support**:

- `get`, `render`, `explain`, `config view`: Full support for `-o` flag
- `run`: Supports `-o` flag for result output
- `auth status`, `secrets list`: Support `-o` flag

---

## Configuration

scafctl uses a configuration file at `~/.scafctl/config.yaml` managed via [Viper](https://github.com/spf13/viper). Configuration can also be set via environment variables with the `SCAFCTL_` prefix.

### Config File Structure

~~~yaml
catalogs:
  - name: default
    type: filesystem
    path: ./
  - name: internal
    type: oci
    url: oci://registry.example.com/scafctl
settings:
  defaultCatalog: default
action:
  # Default output directory for action file operations
  # CLI --output-dir flag overrides this setting
  outputDir: "/path/to/output"
~~~

### Config Commands

View the current configuration:

~~~bash
scafctl config view
~~~

Get a specific setting:

~~~bash
scafctl config get settings.defaultCatalog
~~~

Set a configuration value:

~~~bash
scafctl config set settings.defaultCatalog=internal
~~~

Unset a configuration value:

~~~bash
scafctl config unset settings.defaultCatalog
~~~

### Catalog Management

Convenience commands for catalog configuration:

~~~bash
# Add a catalog
scafctl catalog remote add internal --type oci --url oci://registry.example.com/scafctl

# Remove a catalog
scafctl catalog remote remove internal

# Set the default catalog
scafctl catalog remote default internal
~~~

### Environment Variables

All configuration can be overridden via environment variables:

~~~bash
export SCAFCTL_SETTINGS_DEFAULTCATALOG=internal
export SCAFCTL_CONFIG=/path/to/custom/config.yaml
~~~

---

## Managing Secrets

Securely manage encrypted secrets for authentication and configuration:

~~~bash
# List all secrets
scafctl secrets list

# List all secrets including internal (auth tokens, etc.)
scafctl secrets list --all

# Get a secret value
scafctl secrets get my-api-key

# Get an internal secret (e.g. auth token metadata)
scafctl secrets get scafctl.auth.entra.metadata --all

# Set a secret (prompts for value)
scafctl secrets set my-api-key

# Set with value directly
scafctl secrets set my-api-key --value="secret-value"

# Delete a secret
scafctl secrets delete my-api-key

# Check if secret exists
scafctl secrets exists my-api-key

# Export secrets (encrypted)
scafctl secrets export -o secrets.enc

# Import secrets
scafctl secrets import -i secrets.enc

# Rotate encryption key
scafctl secrets rotate
~~~

Secrets are encrypted with AES-256-GCM and stored in platform-specific locations:

- **macOS**: `~/.local/share/scafctl/secrets/`
- **Linux**: `~/.local/share/scafctl/secrets/`
- **Windows**: `%APPDATA%\scafctl\secrets\`

---

## Authentication

Manage authentication for accessing protected resources:

~~~bash
# Login with an auth handler
scafctl auth login entra

# Check authentication status
scafctl auth status
scafctl auth status entra

# Get a token (for debugging)
scafctl auth token entra --scope "https://graph.microsoft.com/.default"

# Logout
scafctl auth logout entra
~~~

**Supported auth handlers**:

- `entra` - Microsoft Entra ID (formerly Azure AD)

---

## Resolver Commands

> **Note**: The standalone `scafctl resolver graph` command has been removed.
> Use `scafctl run resolver --graph` instead.

### Running Resolvers

The `run resolver` command executes resolvers from a solution without running actions. This is designed for debugging and inspecting resolver execution.

~~~bash
# Run all resolvers
scafctl run resolver -f solution.yaml

# Run specific resolvers (with their transitive dependencies)
scafctl run resolver db config -f solution.yaml

# JSON output (resolver values only, no execution metadata)
scafctl run resolver -f solution.yaml -o json

# JSON output with __execution metadata (phases, timing, providers)
scafctl run resolver -f solution.yaml -o json

# Skip transform and validation phases
scafctl run resolver --skip-transform -f solution.yaml

# Dependency graph (ASCII, DOT, Mermaid, or JSON)
scafctl run resolver --graph -f solution.yaml
scafctl run resolver --graph --graph-format=dot -f solution.yaml

# Snapshot execution state
scafctl run resolver --snapshot --snapshot-file=out.json -f solution.yaml
scafctl run resolver --snapshot --snapshot-file=out.json --redact -f solution.yaml

# Interactive TUI for exploring results
scafctl run resolver -f solution.yaml -i
~~~

Aliases: `res`, `resolvers`

---

## Help and Discovery

List available verbs:

~~~bash
scafctl help
~~~

List supported kinds for a verb:

~~~bash
scafctl run --help
~~~

Get help for a specific kind:

~~~bash
scafctl run solution --help
~~~

Because kinds are registered dynamically, help output always reflects what is available at runtime.

---

## Summary

The scafctl CLI follows a structured, extensible pattern:

- Verbs describe intent
- Kinds identify object types
- Names and versions identify concrete artifacts

This design enables dynamic extension, clear UX, and long-term scalability without breaking existing commands.

---

## Evaluating Expressions

The `eval` command group lets you test CEL expressions, Go templates, and validate solution files without running a full solution.

### Evaluate CEL

~~~bash
# Simple expression
scafctl eval cel "1 + 2"

# With JSON data
scafctl eval cel '_.name == "test"' --data '{"name": "test"}'

# From a data file
scafctl eval cel '_.items.size() > 0' --data-file data.json

# Output as JSON
scafctl eval cel '_.items.filter(i, i.active)' --data-file data.json -o json
~~~

### Evaluate Go Template

~~~bash
# Simple template
scafctl eval template '{{.name}}' --data '{"name": "hello"}'

# Template from file
scafctl eval template --template-file template.txt --data-file data.json

# With output file
scafctl eval template --template-file template.txt --data-file data.json --output result.txt
~~~

### Validate Solution

~~~bash
# Validate a solution YAML file
scafctl eval validate -f solution.yaml

# Output as JSON
scafctl eval validate -f solution.yaml -o json
~~~

---

## Creating New Solutions

Scaffold a new solution from a built-in template:

~~~bash
# Interactive — prompts for name, description, providers
scafctl new solution

# With flags
scafctl new solution --name my-solution --description "My new solution" --output my-solution.yaml

# With specific providers
scafctl new solution --name my-solution --providers static,exec,cel
~~~

---

## Validation Gate

`validate` is THE gate that decides whether a definition is correct and ready.
It runs lint (which includes a JSON Schema conformance check) and turns findings
into a pass/fail result suitable for CI pipelines and pre-commit checks. `lint`
is the advisory subset -- it reports the same authoring findings but is not the
pass/fail gate on its own.

### Validate a Solution

Loads a solution and runs lint. Lint errors (including schema violations) fail;
lint warnings are surfaced but do not fail unless `--strict` is passed:

~~~bash
# Validate the auto-discovered solution (errors fail, warnings surface)
scafctl validate solution

# Validate a specific file
scafctl validate solution -f ./my-solution.yaml

# Treat warnings as fatal too
scafctl validate solution -f ./my-solution.yaml --strict
~~~

### Validate Resolvers

Executes the resolver phases (resolve, transform, validate) and, after they
pass, additionally runs lint as part of the gate. `--strict` makes lint
warnings fatal:

~~~bash
scafctl validate resolver -f ./my-solution.yaml
scafctl validate resolver -f ./my-solution.yaml --strict
~~~

### Validate Data Against a JSON Schema

Validates arbitrary data (JSON or YAML) against a JSON Schema. Unlike
`validate solution`, this does NOT run lint -- it checks raw data conformance
only. Pass `--data -` to read the data from stdin:

~~~bash
# Validate a data file against a schema (both JSON or YAML)
scafctl validate schema --schema schema.json --data data.json

# Read the data from stdin
cat data.yaml | scafctl validate schema --schema schema.json --data -
~~~

Exit codes: `0` conforms, `2` violates the schema, `3` the schema itself is
invalid, `4` a file was not found.

---

## Refactoring Solutions

`refactor` applies source-preserving edits to a solution. It renames a symbol
and rewrites every reference to it, replacing only the exact bytes of each
occurrence, so comments, key order, and formatting are preserved (no YAML
round-trip).

`refactor rename resolver <old> <new>` renames a resolver and rewrites the
definition, `dependsOn` entries, `rslvr:` values, CEL `_.name` uses, and
explicit template `._.name` uses.

`refactor rename action <old> <new>` renames a workflow action and rewrites the
definition, `dependsOn` entries, CEL `__actions.name` uses, and explicit
template `.__actions.name` uses. An action's `alias` is a separate name and is
**not** changed by the rename.

`refactor rename call <old> <new>` renames a reusable call (`spec.calls`) and
rewrites the definition and every `call:` reference (in resolver
with/transform/validate steps and workflow actions). Calls are only referenced
structurally via the `call:` field -- never from CEL or templates.

`refactor rename function <old> <new>` renames an author-defined function
(`spec.functions`) and rewrites the definition and every `{{ name ... }}`
invocation across all templates, including inside other function bodies.
Built-in and extension functions (`printf`, `upper`, ...) that share the new
name are left untouched.

~~~bash
# Preview the change without writing (lists every occurrence and location)
scafctl refactor rename resolver environment env -f ./solution.yaml --dry-run

# Apply it in place (auto-discovers the solution file if -f is omitted)
scafctl refactor rename resolver environment env

# Rename a workflow action and every reference to it
scafctl refactor rename action deploy release

# Rename a reusable call, or an author-defined function
scafctl refactor rename call fetch download
scafctl refactor rename function greet salute
~~~

The rename is all-or-nothing: if any reference to the target symbol cannot be
located byte-exact -- a context-dependent bare `{{ .name }}` accessor, a
`$`-rooted `{{ $.name }}` accessor, or a reference nested inside a literal
value -- it aborts rather than performing a partial rewrite that would silently
break the solution. The check is name- and kind-scoped, so an unlocatable
reference to a *different* symbol (or a resolver that shares an action's name)
does not block the rename.

Exit codes: `0` applied (or previewed), `2` a validation error (invalid new
name, name collision, undefined symbol, or an unlocatable reference blocked
the rename), `4` the solution file could not be resolved, read, or parsed.

### Extract Call (engine)

A second source-preserving refactoring, **Extract Call**, hoists a single
resolve/transform/validate step (a `with[i]` provider+inputs block) out of a
resolver into a reusable `spec.calls` definition and rewrites the selected step
to a `call: <name>` reference. Like rename, it emits byte-exact edits, so
comments and formatting elsewhere are preserved; the extracted block's provider
and inputs (including any inline comments) are spliced verbatim into the new
call, re-indented to the file's style. v1 is conservative: only direct provider
steps are extractable (a step that already uses `call:` is rejected), and no
arguments are inferred -- the call reproduces the inputs literally (inferring
args from near-duplicate steps with varying values is deferred). An opt-in
variant additionally rewrites every *structurally identical* step into the same
`call:` reference; the base extraction never mass-rewrites.

The engine lives in `pkg/refactor` (domain-only, no LSP/protocol imports) and is
designed to back a future LSP `ExtractCall` code action. **The `scafctl refactor
extract-call` CLI subcommand is deferred to a later PR** -- this change ships the
engine only.

---

## Language Server (LSP)

`scafctl lsp` runs a Language Server Protocol server over stdio for editor
integration. It is meant to be launched by an editor / LSP client, not run
interactively -- stdout is the JSON-RPC channel.

~~~bash
# Started by an editor; not typically run by hand
scafctl lsp

# --stdio is accepted for clients that pass it by convention (no-op; stdio is
# the only transport)
scafctl lsp --stdio
~~~

The server gives editors a full authoring experience for solution files, all
consistent with the CLI because it reuses the same `lint` engine, positioned
reference index (`refindex`), rename engine (`refactor`), generated schema, and
built-in function registries.

**Diagnostics** (`textDocument/publishDiagnostics`) -- on open/change/save it
lints the in-memory document and maps each finding's severity, message, and rule
name to an LSP diagnostic anchored at the finding's location. It advertises
full-document sync and reuses the same `lint` engine as `scafctl lint`, so editor
diagnostics match the CLI.

**Navigation and refactoring** over resolver, action, call, and function symbols,
reusing the reference index and rename engine of `refactor rename`:

- **Go-to-definition** (`textDocument/definition`) -- jump from any reference to
  its definition.
- **Find references** (`textDocument/references`) -- list every use of a symbol.
- **Rename** (`textDocument/rename`, with `prepareRename`) -- rename a symbol and
  every reference as a single `WorkspaceEdit`. The same fail-safe applies: if a
  reference cannot be located, the rename is refused and the error is surfaced to
  the editor rather than a partial rewrite being applied.
- **Document outline** (`textDocument/documentSymbol`) -- a `spec` root with
  `resolvers` / `actions` / `calls` / `functions` groups for the Outline pane,
  breadcrumbs, and Go-to-Symbol.

**Authoring assistance** driven by a shared cursor-context resolver:

- **Hover** (`textDocument/hover`) -- symbol descriptions, provider descriptors +
  input schema, CEL/template function signatures, and schema field docs.
- **Completion** (`textDocument/completion`) -- schema-driven keys and enum
  values, CEL/template function names (with call snippets), and symbol names
  after `_.` / `._.` / `call:` / `rslvr:` / `dependsOn`.
- **Signature help** (`textDocument/signatureHelp`) -- declared parameters for CEL
  functions, author/built-in template functions, and a call's `args:`, with the
  active parameter tracking the cursor.
- **Quick fixes** (`textDocument/codeAction`) -- one-click fixes for auto-fixable
  lint diagnostics (deprecated field, redundant `dependsOn`, unused resolver),
  reusing the same fix logic as `scafctl lint --fix`.

Formatting, folding ranges, semantic tokens, and inlay hints are intentional
**non-goals** -- a solution file is YAML, so those are left to the editor's YAML
support (Red Hat YAML / Prettier), keeping the server focused on what only scafctl
can know and preserving other extensions' features on the same file.

An editor client configures the server by pointing at the `scafctl lsp` command
and attaching it to solution files. To keep editor targeting in lockstep with
CLI auto-discovery instead of hardcoding a file list, the server reports the
exact set of file names it recognizes:

~~~bash
# Machine-readable contract consumed by editor integrations
scafctl lsp document-selectors -o json
~~~

This prints the auto-discovered solution/action file names partitioned by editor
language, plus the effective binary name -- covering `solution.{yaml,yml,json}`,
`<binary>.{yaml,yml,json}`, `taskfile.{yaml,yml}`, and `actions.{yaml,yml}`.
JSON solutions are reported separately from YAML so the client attaches them as
JSON documents. The bundled VS Code extension queries this at startup to build
its document selector dynamically (including any embedder binary name), rather
than a hardcoded, drift-prone glob.

---

## Auto-fixing Lint Findings

Some lint rules are **auto-fixable**: `scafctl lint` can rewrite the solution to
resolve them. Fixable rules are marked in `scafctl lint rules` / `scafctl lint
rule <id>` output (a `fixable` field), and the same flag surfaces through the
MCP `list_lint_rules` / `explain_lint_rule` tools.

Three flags drive the fix workflow (modeled on `ruff`'s `--fix` / `--diff`):

~~~bash
# Preview the changes as a unified diff (no write).
scafctl lint --fix-dry-run --diff -f ./solution.yaml

# Report what would change as a summary (no write).
scafctl lint --fix-dry-run -f ./solution.yaml

# Apply the fixes in place.
scafctl lint --fix -f ./solution.yaml
~~~

Semantics:

- A file is written **only** for `--fix` **without** `--diff`. `--fix-dry-run`
  and any `--diff` run are previews and never write.
- `--diff` requires `--fix` or `--fix-dry-run`; `--fix` and `--fix-dry-run` are
  mutually exclusive.
- The diff is emitted to **stdout** (so it can be piped to `patch`); the fix
  summary goes to **stderr** to keep structured stdout clean.
- Fixes reuse the same reference-rewriting engine as `scafctl refactor rename`,
  so comments and formatting are preserved. A fix that would be ambiguous or
  collide with an existing name is **skipped** (reported, not applied) rather
  than producing a broken solution.
- Ambiguous auto-discovery of the solution file is an error (as with `refactor
  rename`); pass `-f`. Reading from stdin (`-`) is not supported for `--fix`.
- **Exit codes** follow `ruff`: `--fix` exits non-zero only when residual errors
  remain after fixing, while the preview modes (`--fix-dry-run`, `--diff`) exit
  non-zero (validation-failed) when there are pending fixes -- so a CI job can
  run `scafctl lint --fix-dry-run` and fail the build when a solution needs
  fixing. A preview exits `0` only when nothing would change.

The `hyphenated-name` rule is currently auto-fixable (it renames a hyphenated
resolver to its camelCase form and rewrites every reference).

---

## Exploring Lint Rules

### List Rules

List all available lint rules with severity, category, and descriptions:

~~~bash
# List all rules
scafctl lint rules

# Output as JSON
scafctl lint rules -o json
~~~

### Explain a Rule

Get a detailed explanation of a specific lint rule:

~~~bash
# Show rule details, examples, and fix guidance
scafctl lint rule <rule-id>

# Output as JSON
scafctl lint rule <rule-id> -o json
~~~

> The former `scafctl lint explain <rule-id>` still works as a hidden,
> deprecated alias for `scafctl lint rule <rule-id>`.

---

## Browsing Examples

Discover and view built-in example solutions. Examples are **embedded in the
binary** (not files on disk), and the listing is driven by each solution's own
`metadata` (displayName, name, category, tags, description) -- only
`kind: Solution` examples are listed.

### List Examples

~~~bash
# List all examples (metadata-driven; prints a tip on how to view one)
scafctl get examples

# Filter by category
scafctl get examples --category solutions
scafctl get examples --category resolvers
scafctl get examples --category actions

# Interactive card + detail view
scafctl get examples -i

# Output as JSON / YAML
scafctl get examples -o json
scafctl get examples -o yaml
~~~

### Get an Example

You can fetch an example by its **path** (always unique) or by its
**metadata.name** / basename when that is unambiguous:

~~~bash
# By full path (always works)
scafctl get examples resolvers/hello-world.yaml

# By metadata.name when unique
scafctl get examples cel-basics

# Save to file
scafctl get examples resolvers/hello-world.yaml > output.yaml
~~~

If a name matches more than one example (e.g. `hello-world` exists as an action,
a resolver, and a solution), the command refuses to guess and lists the
candidate paths so you can pick one:

~~~bash
scafctl get examples hello-world
# error: ambiguous example query: "hello-world" matches
#        actions/hello-world.yaml, resolvers/hello-world.yaml, ...
# Pass the full path to disambiguate.
~~~

---

## Command Grammar: Verb-Noun vs Noun-Verb

### Current State of scafctl

scafctl uses two distinct command grammar patterns:

**Verb-Noun** (kubectl-style) — the verb is the top-level command, the noun follows:

| Command | Verb | Noun |
|---------|------|------|
| `scafctl run solution` | run | solution |
| `scafctl run resolver` | run | resolver |
| `scafctl get solution` | get | solution |
| `scafctl render solution` | render | solution |
| `scafctl inspect solution` | inspect | solution |
| `scafctl package solution` | package | solution |
| `scafctl new solution` | new | solution |
| `scafctl push solution` | push | solution |
| `scafctl pull solution` | pull | solution |
| `scafctl tag solution` | tag | solution |

**Noun-Verb** — the noun is the top-level command, the verb follows:

| Command | Noun | Verb |
|---------|------|------|
| `scafctl secrets get` | secrets | get |
| `scafctl secrets set` | secrets | set |
| `scafctl secrets list` | secrets | list |
| `scafctl secrets delete` | secrets | delete |
| `scafctl auth login` | auth | login |
| `scafctl auth logout` | auth | logout |
| `scafctl auth status` | auth | status |
| `scafctl config view` | config | view |
| `scafctl config set` | config | set |
| `scafctl config get` | config | get |
| `scafctl catalog list` | catalog | list |
| `scafctl catalog inspect` | catalog | inspect |
| `scafctl get snapshot` | get | snapshot |
| `scafctl diff snapshot` | diff | snapshot |
| `scafctl diff solution` | diff | solution |
| `scafctl lint rules` | lint | rules |
| `scafctl lint rule` | lint | rule |
| `scafctl eval cel` | eval | cel |
| `scafctl eval template` | eval | template |
| `scafctl validate solution` | validate | solution |
| `scafctl validate resolver` | validate | resolver |
| `scafctl validate schema` | validate | schema |
| `scafctl get examples` | get | examples |
| `scafctl cache clean` | cache | clean |
| `scafctl plugins list` | plugins | list |
| `scafctl vendor sync` | vendor | sync |

**Standalone** (no sub-noun or sub-verb):

| Command | Notes |
|---------|-------|
| `scafctl version` | informational |
| `scafctl mcp` | launches MCP server |
| `scafctl test` | runs solution tests |

---

### What Major CLIs Do

Most successful CLIs converge on the same hybrid pattern scafctl already uses:

| CLI | Core Domain Objects | Service/Infrastructure | Example |
|-----|---------------------|------------------------|---------|
| **kubectl** | verb-noun: `get pods`, `delete svc`, `apply -f` | noun-verb: `config use-context`, `auth can-i` | Domain is verb-noun; plumbing is noun-verb |
| **docker** | verb-noun: `run`, `build`, `pull`, `push` | noun-verb: `network create`, `volume ls`, `system prune` | Top-level verbs act on images/containers; subsystems are noun-verb |
| **git** | verb-first: `clone`, `commit`, `push`, `pull` | noun-verb: `remote add`, `branch delete`, `stash pop` | Core workflow is verbs; ancillary resource management is noun-verb |
| **gh** (GitHub CLI) | verb-noun: `pr create`, `issue list` | noun-verb: `auth login`, `config set`, `secret set` | Domain objects verb-noun; infrastructure noun-verb |
| **az** (Azure CLI) | noun-verb: `az vm create`, `az storage blob upload` | noun-verb throughout | Purely noun-verb (resource-group style) |
| **gcloud** | noun-verb: `gcloud compute instances create` | noun-verb throughout | Purely noun-verb (resource hierarchy) |
| **terraform** | verb-first: `plan`, `apply`, `destroy` | — | No sub-resources, single verb layer |
| **helm** | verb-noun: `install`, `upgrade`, `rollback` | noun-verb: `repo add`, `plugin install` | Charts are verb-noun; supporting systems noun-verb |

**Observation**: The most widely-used developer CLIs (kubectl, docker, git, gh, helm) all use a **hybrid** model. Only cloud-provider CLIs (az, gcloud) that model deep resource hierarchies go fully noun-verb. Purely verb-noun CLIs (terraform) tend to have a flat, single-resource domain.

---

### Best Practice: The Delineation Rule

The hybrid pattern is not arbitrary — it follows a clear principle:

> **Use verb-noun for domain operations on core business objects.
> Use noun-verb for infrastructure, plumbing, and service management.**

The deciding question: **"Is this a core workflow action the user came here to do, or is it managing supporting infrastructure?"**

| Category | Pattern | Rationale | scafctl examples |
|----------|---------|-----------|-----------------|
| **Core domain operations** | `<verb> <kind>` | The user thinks in terms of *what they want to do*: run, get, render, build. The kind is just a target. | `run solution`, `get provider`, `render solution`, `package solution`, `new solution` |
| **Infrastructure / services** | `<noun> <action>` | The user thinks in terms of *which subsystem* they need to manage. The subsystem is the anchor; actions within it are secondary. | `config set`, `secrets get`, `auth login`, `catalog list`, `cache clean` |
| **Standalone utilities** | `<verb>` or `<noun>` | Single-purpose commands that don't need a sub-resource. | `version`, `mcp`, `test` |

**Why this works:**

1. **Discoverability** — Users can type `scafctl` and immediately see the core verbs (`run`, `get`, `render`) alongside the subsystems (`config`, `secrets`, `auth`). The top-level command list reads like a table-of-contents.
2. **Composability** — Verb-noun allows the same verb to apply to multiple kinds (`run solution`, `run resolver`). Noun-verb allows subsystems to have independent, self-documenting action sets.
3. **Scalability** — New kinds slot into existing verbs. New subsystem features slot into their noun group. Neither pollutes the other.
4. **Precedent** — kubectl, docker, git, gh, and helm all draw the same line, so users already have muscle memory for the split.

---

### Verdict on scafctl

**The current hybrid structure is correct. No changes needed.**

scafctl already follows the established delineation:

- **Verb-noun** for core domain operations — `run`, `get`, `render`, `explain`, `build`, `new`, `push`, `pull`, `tag` all act on domain kinds (solution, provider, resolver).
- **Noun-verb** for infrastructure/services — `config`, `secrets`, `auth`, `catalog`, `snapshot`, `lint`, `eval`, `examples`, `cache`, `plugins`, `vendor` are all subsystem groups with their own actions.
- **Standalone** for single-purpose utilities — `version`, `mcp`, `test`.

This matches how kubectl, docker, git, gh, and helm structure their CLIs. Attempting to "unify" to pure verb-noun or pure noun-verb would:

- **Break user expectations** from other tools.
- **Create awkward commands** — `scafctl manage secrets get` (verb-noun forced) or `scafctl solution run` (noun-verb forced for domain ops) reads worse in both cases.
- **Lose discoverability** — a flat verb-only top level hides subsystem structure; a flat noun-only top level hides the core workflow.

The rule of thumb going forward: if adding a new top-level command, ask *"Is the user performing a core domain action on a kind, or managing a subsystem?"* — verb-noun for the former, noun-verb for the latter.

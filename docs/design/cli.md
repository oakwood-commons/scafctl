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
| `render solution` | ✅ Implemented | Includes graph and snapshot modes |
| `get solution/provider/resolver` | ✅ Implemented | |
| `explain solution/provider` | ✅ Implemented | |
| `config *` | ✅ Implemented | view, get, set, unset, add-catalog, remove-catalog, use-catalog, init, schema, validate |
| `snapshot show/diff` | ✅ Implemented | |
| `secrets *` | ✅ Implemented | list, get, set, delete, exists, export, import, rotate |
| `auth *` | ✅ Implemented | login, logout, status, token |
| `resolver graph` | ❌ Removed | Use `render solution --graph` or `run resolver --graph` instead |
| `build solution` | ✅ Implemented | Catalog feature |
| `catalog list/inspect/delete/prune` | ✅ Implemented | Catalog management |
| `catalog save/load` | ✅ Implemented | Offline distribution |
| `eval cel` | ✅ Implemented | Evaluate CEL expressions from CLI |
| `eval template` | ✅ Implemented | Evaluate Go templates from CLI |
| `eval validate` | ✅ Implemented | Validate solution files from CLI |
| `new solution` | ✅ Implemented | Scaffold a new solution from template |
| `lint rules` | ✅ Implemented | List all available lint rules |
| `lint explain` | ✅ Implemented | Explain a specific lint rule |
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

# List all providers
scafctl get providers

# Get a specific solution
scafctl get solution example
~~~

Both singular and plural forms without a name will list all resources of that kind.
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

---

## Rendering With Parameters

~~~bash
scafctl render solution example \
  -r env=staging \
  -r dryRun=true
~~~

### Render Options

The `render` command supports additional modes for debugging and testing:

#### Dependency Graph

Visualize resolver dependencies without executing:

~~~bash
# ASCII art (default)
scafctl render solution -f solution.yaml --graph

# Graphviz DOT format (pipe to dot command)
scafctl render solution -f solution.yaml --graph --graph-format dot | dot -Tpng > graph.png

# Mermaid diagram syntax
scafctl render solution -f solution.yaml --graph --graph-format mermaid

# JSON for automation
scafctl render solution -f solution.yaml --graph --graph-format json
~~~

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
scafctl snapshot show output.json

# Compare two snapshots
scafctl snapshot diff before.json after.json
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

Build a solution for the local catalog (analogous to `docker build`):

~~~bash
# Build a solution from file
scafctl build solution ./solution.yaml --version 1.0.0

# Build using version from metadata
scafctl build solution ./solution.yaml

# Overwrite existing version
scafctl build solution ./solution.yaml --version 1.0.0 --force
~~~

The build process validates, resolves dependencies, bundles local files, vendors catalog dependencies, and packages artifacts into the local catalog. See [catalog-build-bundling.md](../design/catalog-build-bundling.md) for the full bundling design.

Additional build flags:

~~~bash
# Dry-run: show what would be bundled without building
scafctl build solution ./solution.yaml --dry-run

# Skip file bundling (legacy single-layer artifact)
scafctl build solution ./solution.yaml --no-bundle

# Skip vendoring catalog dependencies
scafctl build solution ./solution.yaml --no-vendor

# Set max bundle size
scafctl build solution ./solution.yaml --bundle-max-size 100MB

# Re-resolve and update the lock file
scafctl build solution ./solution.yaml --update-lock
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

## Explaining Resources

Get detailed metadata and documentation for solutions and providers:

### Explain Solution

~~~bash
# From file
scafctl explain solution -f solution.yaml

# From catalog
scafctl explain solution example
scafctl explain solution example@1.0.0
~~~

Outputs:
- Name, version, description
- List of resolvers with their providers
- List of actions with types
- Required parameters
- Dependency summary

### Explain Provider

~~~bash
scafctl explain provider github
scafctl explain provider static
~~~

Outputs:
- Provider description
- Configuration schema with types and validation
- Example configurations
- Supported features

---

## Global Flags

These flags are available on most commands:

| Flag | Short | Description | Status |
|------|-------|-------------|--------|
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
scafctl config add-catalog internal --type=oci --url=oci://registry.example.com/scafctl

# Remove a catalog
scafctl config remove-catalog internal

# Set the default catalog
scafctl config use-catalog internal
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
> Use `scafctl render solution --graph` or `scafctl run resolver --graph` instead.

### Running Resolvers

The `run resolver` command executes resolvers from a solution without running actions. This is designed for debugging and inspecting resolver execution.

~~~bash
# Run all resolvers
scafctl run resolver -f solution.yaml

# Run specific resolvers (with their transitive dependencies)
scafctl run resolver db config -f solution.yaml

# JSON output (includes __execution metadata by default)
scafctl run resolver -f solution.yaml -o json

# JSON output without __execution metadata
scafctl run resolver -f solution.yaml -o json --hide-execution

# Skip transform and validation phases
scafctl run resolver --skip-transform -f solution.yaml

# Show execution plan without running
scafctl run resolver --dry-run -f solution.yaml

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
scafctl lint explain <rule-id>

# Output as JSON
scafctl lint explain <rule-id> -o json
~~~

---

## Browsing Examples

Discover and download built-in example configurations:

### List Examples

~~~bash
# List all examples
scafctl examples list

# Filter by category
scafctl examples list --category solutions
scafctl examples list --category resolvers
scafctl examples list --category actions

# Output as JSON
scafctl examples list -o json

# Output as YAML
scafctl examples list -o yaml
~~~

### Get an Example

~~~bash
# Print example to stdout
scafctl examples get resolvers/hello-world.yaml

# Save to file
scafctl examples get resolvers/hello-world.yaml -o output.yaml
~~~

Aliases: `ls` for list

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
| `scafctl explain solution` | explain | solution |
| `scafctl build solution` | build | solution |
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
| `scafctl snapshot show` | snapshot | show |
| `scafctl snapshot diff` | snapshot | diff |
| `scafctl lint rules` | lint | rules |
| `scafctl lint explain` | lint | explain |
| `scafctl eval cel` | eval | cel |
| `scafctl eval template` | eval | template |
| `scafctl examples list` | examples | list |
| `scafctl examples get` | examples | get |
| `scafctl cache clean` | cache | clean |
| `scafctl plugins list` | plugins | list |
| `scafctl bundle create` | bundle | create |
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
| **Core domain operations** | `<verb> <kind>` | The user thinks in terms of *what they want to do*: run, get, render, build. The kind is just a target. | `run solution`, `get provider`, `render solution`, `build solution`, `new solution` |
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
- **Noun-verb** for infrastructure/services — `config`, `secrets`, `auth`, `catalog`, `snapshot`, `lint`, `eval`, `examples`, `cache`, `plugins`, `bundle`, `vendor` are all subsystem groups with their own actions.
- **Standalone** for single-purpose utilities — `version`, `mcp`, `test`.

This matches how kubectl, docker, git, gh, and helm structure their CLIs. Attempting to "unify" to pure verb-noun or pure noun-verb would:

- **Break user expectations** from other tools.
- **Create awkward commands** — `scafctl manage secrets get` (verb-noun forced) or `scafctl solution run` (noun-verb forced for domain ops) reads worse in both cases.
- **Lose discoverability** — a flat verb-only top level hides subsystem structure; a flat noun-only top level hides the core workflow.

The rule of thumb going forward: if adding a new top-level command, ask *"Is the user performing a core domain action on a kind, or managing a subsystem?"* — verb-noun for the former, noun-verb for the latter.

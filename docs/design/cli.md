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
| `run solution` | ✅ Implemented | Full support with actions |
| `render solution` | ✅ Implemented | Includes graph and snapshot modes |
| `get solution/provider/resolver` | ✅ Implemented | |
| `explain solution/provider` | ✅ Implemented | |
| `config *` | ✅ Implemented | view, get, set, unset, add-catalog, remove-catalog, use-catalog, init, schema, validate |
| `snapshot show/diff` | ✅ Implemented | |
| `secrets *` | ✅ Implemented | list, get, set, delete, exists, export, import, rotate |
| `auth *` | ✅ Implemented | login, logout, status, token |
| `resolver graph` | ✅ Implemented | Standalone graph visualization |
| `build solution/plugin` | 📋 Planned | Catalog feature |
| `push solution/plugin` | 📋 Planned | Catalog feature |
| `pull solution/plugin` | 📋 Planned | Catalog feature |
| `inspect solution/plugin` | 📋 Planned | Catalog feature |
| `tag solution/plugin` | 📋 Planned | Catalog feature |
| `save/load` | 📋 Planned | Offline distribution |
| `delete solution` | 📋 Planned | Catalog feature |
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

Execute a solutions resolver and perform its actions.

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

> **Status**: 📋 Planned - Catalog functionality is not yet implemented.
> Currently, solutions are loaded from local files using `-f/--file`.

Run a solution directly from a catalog:

~~~bash
scafctl run solution example@1.7.0
~~~

### Building Artifacts

> **Status**: 📋 Planned

Build a solution or plugin for the local catalog (analogous to `docker build`):

~~~bash
# Build a solution from file
scafctl build solution -f ./solution.yaml

# Build a plugin
scafctl build plugin -f ./plugin-config.yaml
~~~

The build process validates, resolves dependencies, and packages artifacts into the local catalog.

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

> **Status**: 📋 Planned

View artifact metadata, dependencies, and structure:

~~~bash
# Inspect a solution
scafctl inspect solution example@1.7.0

# Inspect a plugin (shows available providers)
scafctl inspect plugin aws-provider@1.5.0
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

> **Status**: 📋 Planned

Export and import artifacts for air-gapped environments (analogous to `docker save/load`):

~~~bash
# Save a solution with dependencies
scafctl save solution my-solution@1.2.3 -o solution.tar

# Save a plugin
scafctl save plugin aws-provider@1.5.0 -o plugin.tar

# Load from archive
scafctl load -i solution.tar
scafctl load -i plugin.tar
~~~

### Deleting Solutions

> **Status**: 📋 Planned

Remove a solution from a catalog:

~~~bash
# Delete specific version
scafctl delete solution example@1.7.0

# Delete from specific catalog
scafctl delete solution example@1.7.0 --catalog=staging
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
| `--log-level` | | Set log level (-1=Debug, 0=Info, 1=Warn, 2=Error) | ✅ Implemented |
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

# Get a secret value
scafctl secrets get my-api-key

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
- **macOS**: `~/Library/Application Support/scafctl/secrets/`
- **Linux**: `~/.config/scafctl/secrets/`
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

Standalone resolver utilities:

~~~bash
# Visualize resolver dependency graph from file
scafctl resolver graph -f solution.yaml

# Different output formats
scafctl resolver graph -f solution.yaml --format=dot
scafctl resolver graph -f solution.yaml --format=mermaid
scafctl resolver graph -f solution.yaml --format=json
~~~

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

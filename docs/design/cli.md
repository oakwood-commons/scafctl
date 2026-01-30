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

## Core Concepts

### Verbs

Common verbs include:

- `run`     execute actions with side effects
- `render`  evaluate resolvers and actions without side effects
- `explain` Get documentation for a resource
- `publish` publish artifacts to a catalog
- `get` Display one or many resources

Verbs are global and apply to all supported kinds.

---

### Kinds

Kinds are dynamically registered at runtime. Built-in kinds include:

- `solution`
- `provider`
- `catalog`
- `plugin`

Additional kinds may be introduced by plugins without changing the CLI.

---

### Names and Versions

Names identify an object within a kind.

Versions are optional and may be:
- an exact version (`1.0.0`)
- a constraint (`^1.2`, `>=1.0 <2.0`)
- omitted (default resolution rules apply)

---

## Running a Solution

Execute a solution and perform its actions.

~~~bash
scafctl run solution example
~~~

Locates the solution.yaml on the local file system and runs it.

~~~bash
scafctl run solution
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

## Rendering a Solution

Render evaluates resolvers and actions but does not perform side effects.

~~~bash
scafctl render solution example
~~~

Render solution.yaml from local file system

~~~bash
scafctl render solution 
~~~

get from stdin

~~~bash
cat solution1.yaml | scafctl render solution -f -
~~~

get from a specific file

~~~bash
scafctl render solution -f mysolution.yaml
~~~

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

---

## Working With the Catalog

Run a solution directly from a catalog:

~~~bash
scafctl run solution example@1.7.0
~~~

Publish a solution to a catalog:

~~~bash
scafctl publish solution example@1.7.0
~~~

Catalog resolution rules determine whether artifacts are loaded locally or remotely.

---

## Providers and Other Kinds

explain a provider:

~~~bash
scafctl explain provider github
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

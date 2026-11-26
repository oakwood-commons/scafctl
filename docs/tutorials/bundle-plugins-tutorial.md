---
title: "Bundle Plugins & Built-in Providers"
weight: 136
---

# Bundle Plugins & Built-in Providers

This tutorial explains the `bundle.plugins` section, how it interacts with
built-in providers, and how to avoid the common mistake of declaring a
built-in provider as a plugin dependency.

## Overview

scafctl ships with a set of **built-in providers** compiled into the binary.
These are always available without any plugin declaration:

| Built-in Provider |
|-------------------|
| `cel` |
| `debug` |
| `file` |
| `go-template` |
| `http` |
| `message` |
| `parameter` |
| `solution` |
| `state` |
| `static` |
| `validation` |

**External providers** (e.g., `directory`, `env`, `exec`, `git`, `github`,
`hcl`, `identity`, `kubeconfig`, `metadata`, `secret`, `sleep`) are distributed as catalog
plugins and must be declared in `bundle.plugins` so scafctl can fetch them at
runtime. See the [Plugin Auto-Fetching](../plugin-auto-fetch-tutorial/) tutorial
for details on how plugin resolution works.

## The Problem: Built-in Provider in bundle.plugins

A common mistake is declaring a built-in provider in `bundle.plugins`:

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution
  version: 1.0.0

# BAD: cel is a built-in provider -- do not declare it here.
bundle:
  plugins:
    - name: cel
      kind: provider
      version: "1.0.0"

spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hello world'"
~~~

This is **unnecessary and misleading**. The `cel` provider is already compiled
into the binary and does not need to be fetched. Declaring it:

1. Is silently skipped by the plugin fetcher at runtime (builtins are
   filtered out before catalog resolution).
2. Suggests the solution depends on an external plugin when it does not.

At runtime, scafctl silently skips built-in entries in `bundle.plugins`, so
the solution still works -- but the declaration is noise.

## Detection: The `builtin-in-bundle-plugins` Lint Rule

scafctl's linter catches this mistake automatically:

~~~shell
scafctl lint -f ./solution.yaml
~~~

Output:

~~~
WARNING  bundle  builtin-in-bundle-plugins
  bundle.plugins entry "cel" is a builtin provider and will be ignored at runtime
  Fix: Remove this entry; builtin providers are always available without an explicit plugin declaration
~~~

The rule fires for every `bundle.plugins` entry whose `name` matches a
built-in provider and whose `kind` is `provider`.

### Getting Help on the Rule

To see a detailed explanation with examples:

~~~shell
scafctl lint rule builtin-in-bundle-plugins
~~~

Or via the MCP server:

~~~
explain_lint_rule(ruleName: "builtin-in-bundle-plugins")
~~~

## The Fix

Remove built-in provider entries from `bundle.plugins`. Only declare
**external** (catalog-distributed) providers:

~~~yaml
# CORRECT: only declare external plugins
bundle:
  plugins:
    - name: aws-provider
      kind: provider
      version: "^1.5.0"
~~~

By default an external plugin is resolved by its short `name` through the
configured plugin catalog chain:

1. Locally built plugins in the `local` catalog (`scafctl package plugin`)
2. Configured user or organization OCI catalogs (in configuration order)
3. The `official` catalog (`oci://ghcr.io/oakwood-commons`), unless disabled

Both runtime execution (`scafctl run solution`) and solution packaging
(`scafctl package solution`) walk this same chain automatically -- you do **not**
need to run `scafctl catalog pull` beforehand to vendor short-name plugins into
`solution.lock`. At package time, `scafctl package solution` resolves the
matching version and multi-platform content digests directly from the winning
catalog's OCI manifests (without downloading plugin binaries) and records
`resolvedFrom` and `resolvedCanonical` in `solution.lock`.

To pin a plugin to an explicit OCI registry instead, add a `source` block
(`source.registry` / `source.artifact`):

~~~yaml
bundle:
  plugins:
    - name: exec                        # solution-local alias used as the provider handle
      kind: provider
      version: "^1.5.0"                  # semver constraint or "latest"
      source:
        registry: ghcr.io/oakwood-commons   # OCI registry + namespace
        artifact: scafctl-exec-provider     # artifact leaf name within the registry
~~~

Here `name` is only the solution-local handle you reference from a resolver or
action (`provider: exec`); the real OCI coordinates are `source.registry` +
`source.artifact`. When `artifact` differs from `name` the dependency is
"aliased". `source.registry` must map to a **configured catalog**. See
[Pinning a plugin to a specific registry](../plugin-auto-fetch-tutorial/#pinning-a-plugin-to-a-specific-registry)
in the Plugin Auto-Fetching tutorial for the full field reference and how the
origin is recorded in the lock file.

To require a locally built copy from the `local` catalog (`source.built`), set
`built: true` instead of `registry`:

~~~yaml
bundle:
  plugins:
    - name: my-local-provider
      kind: provider
      version: "^0.1.0"
      source:
        built: true
~~~

If your solution only uses built-in providers, omit `bundle.plugins` entirely:

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
~~~

## Updating Locked Plugins

Once `scafctl package solution` writes `solution.lock`, subsequent builds replay
pinned plugin versions that still satisfy the constraints in `bundle.plugins` so
packaging stays reproducible. When a newer compatible plugin release is
published to a configured catalog, use `scafctl vendor update` to check for and
apply upgrades without manually editing or deleting `solution.lock`:

~~~shell
# Preview available plugin and sub-solution upgrades without modifying files
scafctl vendor update --dry-run

# Emit a structured JSON update report on stdout (progress goes to stderr)
scafctl vendor update --dry-run -o json

# Upgrade all locked plugins and vendored sub-solutions within their constraints
scafctl vendor update

# Upgrade a single plugin while keeping all other lock entries pinned as-is
scafctl vendor update --dependency exec

# Disambiguate when a provider and auth-handler share the same short name
scafctl vendor update --dependency auth-handler/github

# Include pre-release versions when resolving 'latest' (or use a -0 range such as ^0.1.0-0)
scafctl vendor update --pre-release
~~~

`scafctl vendor update` queries live remote catalogs directly (bypassing local
pull-through cache fallbacks for remote plugins and sub-solutions while still
honoring locally built artifacts), respects `source.registry`, `source.built`,
short-name catalog chain priority, and active plugin signature policies, writes
updated sub-solution files and prunes superseded vendor files (with `--lock-only`
pins materialized automatically on the next `scafctl package solution` build),
and leaves `solution.lock` untouched when every dependency is already up to date.

## Mixing Built-in and External Providers

When a solution uses both built-in and external providers, only declare the
external ones:

~~~yaml
bundle:
  plugins:
    # Only external providers belong here:
    - name: exec
      kind: provider
      version: "^1.0.0"
    # Do NOT add: cel, static, file, http, etc.

spec:
  resolvers:
    timestamp:
      resolve:
        with:
          # Built-in -- no plugin declaration needed
          - provider: cel
            inputs:
              expression: "now()"
    script-output:
      resolve:
        with:
          # External -- declared in bundle.plugins above
          - provider: exec
            inputs:
              command: echo
              args: ["hello"]
~~~

## Example Solution

A complete example demonstrating the lint warning is available at
[examples/plugins/builtin-in-bundle-plugins.yaml](https://github.com/oakwood-commons/scafctl/blob/main/examples/plugins/builtin-in-bundle-plugins.yaml).

~~~shell
# See the lint warning
scafctl lint -f ./examples/plugins/builtin-in-bundle-plugins.yaml

# Run resolvers (succeeds -- builtin entries are silently skipped)
scafctl run resolver -f ./examples/plugins/builtin-in-bundle-plugins.yaml -o json
~~~

## Summary

- **Built-in providers** are always available -- never declare them in `bundle.plugins`.
- **External providers** must be declared in `bundle.plugins` for auto-fetching.
- The `builtin-in-bundle-plugins` lint rule catches the mistake automatically.
- At runtime, the plugin fetcher silently skips built-in entries, so existing
  solutions are not broken -- but the unnecessary declaration should be removed.

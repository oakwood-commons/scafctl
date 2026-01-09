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
  -r regions=us-east1,us-west1
~~~

Parameters participate in normal resolver resolution via the `parameter` provider.

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

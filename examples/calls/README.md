# Parameterized Call Examples

These examples demonstrate `spec.calls` -- reusable, argument-driven provider
requests. A call is defined once with typed `args` and invoked from any resolve,
transform, validate, or action step via `call:` + `args:` instead of
`provider:` + `inputs:`.

## Examples

| Example | Description |
|---------|-------------|
| [calls-basics.yaml](calls-basics.yaml) | One definition, several call sites; typed args with defaults and required values; the `args` namespace; a call in a validate step with `dedup`. |
| [calls-action.yaml](calls-action.yaml) | A call invoked from workflow actions, reused with different arguments. |

## Key Concepts

- **Call definition** (`spec.calls.<name>`) -- a named, reusable request. Declares
  typed `args`, a `provider`, and provider `inputs` that reference arguments via
  the `args` namespace.
- **Call site** (`call:` + `args:`) -- a step or action that invokes a definition,
  supplying argument values as standard `ValueRef`s (bare scalar literals,
  `expr:`, `tmpl:`, or `rslvr:`).
- **`args` namespace** -- inside a definition's inputs, reference arguments as
  `_.args.x` (CEL) and `{{ .args.x }}` (Go template).
- **`dedup: true`** -- identical invocations (same definition, same bound args)
  within a single run execute the provider only once and share the result.

## Run

~~~bash
# Resolver-only example
scafctl run resolver -f examples/calls/calls-basics.yaml -o yaml

# Workflow (action) example
scafctl run solution -f examples/calls/calls-action.yaml
~~~

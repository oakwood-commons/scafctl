---
title: "Working Directory (--cwd)"
weight: 16
---

# Working Directory (`--cwd`)

## Overview

The `--cwd` (short: `-C`) global flag changes the logical working directory for all path resolution without mutating the process CWD. It behaves similarly to `git -C`:

```bash
scafctl --cwd /path/to/project run solution -f solution.yaml
scafctl -C /path/to/project run resolver -f demo.yaml -o json
```

When `--cwd` is set, **all** relative paths—including `-f`, `--output-dir`, snapshot paths, and auto-discovery—resolve against the specified directory instead of the process CWD.

---

## Architecture

### Context-Based Resolution

The `--cwd` flag is injected into the Go `context.Context` via `provider.WithWorkingDirectory()`, avoiding global state mutation. This design:

- Is safe for concurrent use (MCP server handles multiple requests)
- Does not affect other goroutines or the process environment
- Composes cleanly with `--output-dir` (which operates on a separate context key)

### Resolution Flow

```
CLI:  --cwd flag → root PersistentPreRunE → provider.WithWorkingDirectory(ctx)
MCP:  cwd param  → s.contextWithCwd()     → provider.WithWorkingDirectory(ctx)
                                                    ↓
                                          provider.AbsFromContext(ctx, path)
                                                    ↓
                                          filepath.Join(cwd, relativePath)
```

### Key Functions

| Function | Package | Purpose |
|----------|---------|---------|
| `WithWorkingDirectory` | `pkg/provider` | Stores the working directory in context |
| `WorkingDirectoryFromContext` | `pkg/provider` | Retrieves the working directory from context |
| `AbsFromContext` | `pkg/provider` | Resolves a relative path against the context working directory |
| `GetWorkingDirectory` | `pkg/provider` | Returns the effective working directory (context → `os.Getwd()` fallback) |
| `ResolvePath` | `pkg/provider` | Full path resolution including output-dir and cwd support |

---

## Interaction with `--output-dir`

The `--cwd` and `--output-dir` flags serve different purposes and compose together:

| Flag | Scope | Affects |
|------|-------|---------|
| `--cwd` | All phases (resolvers + actions) | Where relative paths are resolved from |
| `--output-dir` | Action phase only | Where action outputs are written to |

When both are set:
- **Resolvers** resolve paths against `--cwd`
- **Actions** resolve output paths against `--output-dir`
- A relative `--output-dir` itself is resolved against `--cwd`

```bash
# Resolvers read from /project, actions write to /project/out
scafctl --cwd /project run solution -f sol.yaml --output-dir out
```

---

## MCP Server

All MCP tools that accept file paths support an optional `cwd` parameter. When provided, it behaves identically to the CLI `--cwd` flag:

```json
{
  "tool": "render_solution",
  "arguments": {
    "path": "solution.yaml",
    "cwd": "/path/to/project"
  }
}
```

### Tools with `cwd` Support

| Tool | File |
|------|------|
| `inspect_solution` | `tools_solution.go` |
| `lint_solution` | `tools_solution.go` |
| `render_solution` | `tools_solution.go` |
| `preview_resolvers` | `tools_solution.go` |
| `run_solution_tests` | `tools_solution.go` |
| `get_run_command` | `tools_solution.go` |
| `preview_action` | `tools_action.go` |
| `dryrun_solution` | `tools_dryrun.go` |
| `show_snapshot` | `tools_snapshot.go` |
| `diff_snapshots` | `tools_snapshot.go` |
| `generate_test_scaffold` | `tools_testing.go` |
| `list_tests` | `tools_testing.go` |
| `diff_solution` | `tools_diff.go` |
| `build_plugin` | `tools_catalog_multiplatform.go` |
| `extract_resolver_refs` | `tools_refs.go` |

### Tools without `cwd` (not applicable)

| Tool | Reason |
|------|--------|
| `list_examples` / `get_example` | Paths reference embedded resources, not the filesystem |
| `catalog_list` / `catalog_list_platforms` | Catalog references, not filesystem paths |
| `list_providers` / `get_provider_schema` | In-memory registry lookups |
| `list_cel_functions` / `evaluate_cel` | No filesystem paths |
| `auth_status` | No filesystem paths |

---

## Fallback Behavior

When `--cwd` is **not** set:
1. `WorkingDirectoryFromContext` returns `("", false)`
2. `AbsFromContext` falls through to `filepath.Abs()` which uses `os.Getwd()`
3. Behavior is identical to running without `--cwd`—fully backward compatible

---

## Validation

The `--cwd` value is validated at entry point (CLI root or MCP `contextWithCwd`):

1. Resolved to absolute path via `filepath.Abs`
2. Checked for existence via `os.Stat`
3. Verified to be a directory (not a file)

Invalid values produce a clear error before any command logic executes.

---

## Catalog Solution CWD

When running a catalog solution (bare name), the bundle is extracted to a temporary
directory and the process CWD is changed there so resolvers can read bundled files.

However, file-writing **actions** need to resolve relative paths against the
caller's original working directory — not the temporary bundle directory. The CLI
injects the caller's CWD into the action execution context via
`provider.WithWorkingDirectory(actionCtx, originalCwd)`. This ensures
`AbsFromContext` resolves relative action paths against the caller's CWD, matching
the behaviour of local `-f` file runs.

```
Catalog run:  scafctl run solution my-app

1. User is in /projects/my-app (caller CWD)
2. Bundle extracted to /tmp/scafctl-bundle-xyz/
3. os.Chdir(/tmp/scafctl-bundle-xyz/)   ← resolvers read bundled files here
4. originalCwd captured as /projects/my-app
5. actionCtx = WithWorkingDirectory(ctx, originalCwd)
6. Actions resolve "./output" → /projects/my-app/output  ← correct
```

This aligns with how tools like `npm init`, `cargo init`, and `cookiecutter` work
— they scaffold into the directory you run them from, regardless of where the
template source lives.

When `--output-dir` is specified, it takes precedence over the context CWD for
action path resolution (unchanged behaviour).

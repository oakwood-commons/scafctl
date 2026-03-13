---
title: "Output Directory"
weight: 15
---

# Output Directory

## Overview

The `--output-dir` flag scopes the action/workflow phase to a target directory, creating a clear phase-based separation:

- **Resolvers** always operate in CWD (current working directory) — they gather data
- **Actions** operate in the output directory when `--output-dir` is set — they produce output

When `--output-dir` is not set, actions continue using CWD (backward compatible).

---

## Mental Model

```
CWD (current working directory)
├── solution.yaml           ← parsed by scafctl
├── templates/              ← read by resolvers
│   └── app.yaml.tpl
└── data/                   ← read by resolvers
    └── config.json

--output-dir /path/to/output
├── app.yaml                ← written by actions
├── config/
│   └── settings.json       ← written by actions
└── scripts/
    └── deploy.sh           ← written by actions
```

### Phase Semantics

| Phase | Directory | Behavior |
|-------|-----------|----------|
| **Resolver** | CWD (always) | `file` provider reads from CWD, `directory` lists CWD, etc. |
| **Action** (with `--output-dir`) | Output directory | `file` write goes to output-dir, `directory` mkdir goes to output-dir |
| **Action** (without flag) | CWD | Same as today — full backward compatibility |

---

## Usage

### CLI Flag

```bash
# Actions write to /tmp/output instead of CWD
scafctl run solution -f solution.yaml --output-dir /tmp/output

# Dry-run with output-dir shows target paths
scafctl run solution -f solution.yaml --output-dir /tmp/output --dry-run

# Run resolvers only (--output-dir accepted but has no effect)
scafctl run resolver -f solution.yaml --output-dir /tmp/output -o json

# Run a specific provider in action mode with output-dir
scafctl run provider file --capability action --output-dir /tmp/output \
  -i '{"operation": "write", "path": "hello.txt", "content": "world"}'
```

### Default Setting

A default output directory can be configured in the app config so you don't need to pass the flag every time:

```yaml
# In scafctl config
action:
  outputDir: /path/to/default/output
```

The CLI flag always overrides the configured default.

---

## The `__cwd` Escape Hatch

When `--output-dir` is set, actions resolve relative paths against the output directory. But sometimes an action needs to reference the original working directory — for example, to read a script or reference source files.

The `__cwd` built-in variable provides the original working directory path inside action expressions:

```yaml
workflow:
  actions:
    run-script:
      provider: exec
      inputs:
        command: bash
        args:
          expr: '[__cwd + "/scripts/deploy.sh"]'

    copy-readme:
      provider: file
      inputs:
        operation: write
        path: README.md
        content:
          expr: '"Source: " + __cwd + "/README.md"'
```

`__cwd` is available alongside `__actions` in all action CEL expressions and Go templates.

---

## Implementation Details

### Path Resolution

All filesystem providers use `provider.ResolvePath(ctx, path)` to resolve paths:

```
IF path is absolute → return filepath.Clean(path)
ELSE IF (execution mode == action) AND (output-dir is set) AND (path is relative)
    → return filepath.Join(outputDir, path)
ELSE
    → return filepath.Abs(path)  // CWD-based
```

This means:
- Absolute paths always resolve to themselves (no rewriting)
- Relative paths in action mode resolve against `--output-dir`
- Relative paths in resolver mode always resolve against CWD
- When `--output-dir` is not set, everything resolves against CWD

### Affected Providers

| Provider | Operations Affected |
|----------|-------------------|
| `file` | read, write, exists, delete, write-tree |
| `directory` | list, mkdir, rmdir, copy |
| `exec` | workingDir defaults to output-dir in action mode |
| `hcl` | parse, format, validate, list |

### Provider Not Affected

| Provider | Reason |
|----------|--------|
| `solution` | `Canonicalize` is for ancestry tracking, not user filesystem ops |

### Exec Provider Special Handling

The `exec` provider has special behavior for `workingDir`:

- **Empty `workingDir`** in action mode with `--output-dir` → defaults to output-dir
- **Relative `workingDir`** in action mode with `--output-dir` → resolved via `ResolvePath`
- **Absolute `workingDir`** → no change (always honored as-is)

---

## Auto-Creation

When `--output-dir` is specified, the directory is automatically created (including nested parents) before execution begins. If creation fails, execution is aborted with an error.

---

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Phase-based model | Resolvers always CWD, actions always output-dir. No read/write distinction needed. |
| Single `ResolvePath` helper | Checks execution mode + output-dir context. Same function used by all providers. |
| `__cwd` escape hatch | Injected as built-in variable for action expressions needing original CWD. |
| Exec default workingDir | Empty workingDir in action mode defaults to output-dir. |
| Backward compatible | No `--output-dir` → all paths resolve via CWD, exactly as before. |
| Auto-create | `os.MkdirAll` before execution starts. Fail-fast on error. |

---

## Migration

Existing solutions require **no changes**. The `--output-dir` flag is opt-in:

- Without the flag → identical behavior to before
- With the flag → actions write to the specified directory

If an existing solution uses absolute paths in actions, those paths are unaffected by `--output-dir` (absolute paths are never rewritten).

---

## Examples

See the [output-dir example](../../../examples/solutions/output-dir/) for a complete working solution demonstrating the phase-based directory model.

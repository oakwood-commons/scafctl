# Output Directory Example

Demonstrates the phase-based output directory model introduced by the `--output-dir` flag.

## Concept

scafctl's execution model has two distinct phases with different directory semantics:

| Phase | Directory | Purpose |
|-------|-----------|---------|
| **Resolvers** | CWD (always) | Gather data — read-only, no side effects |
| **Actions** | `--output-dir` (when set) | Produce output — write files, execute commands |

When `--output-dir` is not set, actions use CWD (backward compatible).

## Features Demonstrated

| Feature | Description |
|---------|-------------|
| Resolver reads from CWD | `source-content` reads a file relative to the current directory |
| Action writes to output-dir | `write-greeting` and `write-config` write files into the output directory |
| `__cwd` escape hatch | `write-cwd-ref` uses the `__cwd` built-in to reference the original directory |
| Template rendering | Resolvers render a template; actions write the result to output-dir |

## Running

```bash
# Run with output directory (actions write here)
scafctl run solution -f examples/solutions/output-dir/solution.yaml --output-dir /tmp/output-dir-demo

# Inspect results
cat /tmp/output-dir-demo/greeting.txt
cat /tmp/output-dir-demo/config/app.yaml
cat /tmp/output-dir-demo/cwd-reference.txt

# Run without --output-dir (actions write to CWD, backward compatible)
scafctl run solution -f examples/solutions/output-dir/solution.yaml

# Dry-run to preview what would happen
scafctl run solution -f examples/solutions/output-dir/solution.yaml --output-dir /tmp/output-dir-demo --dry-run

# Run resolvers only (--output-dir has no effect on resolvers)
scafctl run resolver -f examples/solutions/output-dir/solution.yaml -o json
```

## Directory Model

```
CWD (current working directory)
├── solution.yaml          ← solution definition
├── source.txt             ← read by resolver (always CWD)
└── ...

--output-dir /tmp/output-dir-demo
├── greeting.txt           ← written by action
├── config/
│   └── app.yaml           ← written by action (subdirectory auto-created)
└── cwd-reference.txt      ← contains __cwd value (original CWD path)
```

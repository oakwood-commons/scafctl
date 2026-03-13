---
title: "Output Directory Tutorial"
weight: 87
---

# Output Directory Tutorial

This tutorial walks you through using the `--output-dir` flag to redirect action output to a specific directory. You'll learn the phase-based directory model, how resolvers and actions differ in path handling, and how to reference the original working directory with `__cwd`.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with YAML syntax and solution files
- Completion of the [Getting Started](../getting-started/) tutorial

## Table of Contents

1. [Overview](#overview)
2. [Phase-Based Directory Model](#phase-based-directory-model)
3. [Basic Usage](#basic-usage)
4. [Using `__cwd` for Original Directory Access](#using-__cwd-for-original-directory-access)
5. [Combining with Dry-Run](#combining-with-dry-run)
6. [Per-Provider Usage](#per-provider-usage)
7. [Default via Configuration](#default-via-configuration)
8. [Absolute Paths Are Never Rewritten](#absolute-paths-are-never-rewritten)
9. [Affected Providers](#affected-providers)
10. [Common Patterns](#common-patterns)

---

## Overview

By default, actions resolve relative paths against the current working directory (CWD). The `--output-dir` flag changes this so actions write to a designated output directory instead, while resolvers continue reading from CWD.

This is useful when you want to:

- Keep your source directory clean while generating output elsewhere
- Write generated files to a build or staging directory
- Separate "read" (resolver) and "write" (action) phases to different locations

---

## Phase-Based Directory Model

scafctl execution has two phases with different directory semantics:

| Phase | Directory | Purpose |
|-------|-----------|---------|
| **Resolvers** | CWD (always) | Gather data — read files, query APIs, compute values |
| **Actions** | `--output-dir` (when set) | Produce output — write files, run commands, create directories |

When `--output-dir` is not set, actions also use CWD (fully backward compatible).

```
CWD (current working directory)
├── solution.yaml           ← parsed by scafctl
├── templates/              ← read by resolvers
│   └── app.yaml.tpl
└── data/                   ← read by resolvers
    └── config.json

--output-dir /tmp/output
├── app.yaml                ← written by actions
├── config/
│   └── settings.json       ← written by actions
└── scripts/
    └── deploy.sh           ← written by actions
```

---

## Basic Usage

Create a solution with a resolver that reads from CWD and an action that writes to the output directory.

### Step 1: Create Source Files

Create a project directory with a source file:

```bash
mkdir -p output-dir-demo && cd output-dir-demo

cat > source.txt <<'EOF'
Hello from the source directory!
EOF
```

### Step 2: Create the Solution

Create `solution.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: output-dir-demo
  version: 1.0.0

spec:
  resolvers:
    # Resolvers always read from CWD
    source-content:
      type: any
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: source.txt

  workflow:
    actions:
      # Actions write to --output-dir when set
      write-output:
        provider: file
        inputs:
          operation: write
          path: result.txt
          content:
            rslvr: source-content
```

### Step 3: Run with `--output-dir`

```bash
scafctl run solution -f solution.yaml --output-dir /tmp/demo-output
```

### Step 4: Verify

```bash
# The output file is in the output directory, NOT in CWD
cat /tmp/demo-output/result.txt
# Output: Hello from the source directory!

# CWD is unchanged — no result.txt here
ls -la
# Only source.txt and solution.yaml
```

The resolver read `source.txt` from CWD. The action wrote `result.txt` to `/tmp/demo-output`.

---

## Using `__cwd` for Original Directory Access

When `--output-dir` is active, actions resolve relative paths against the output directory. But sometimes an action needs to reference the original working directory — for example, to run a script that lives alongside the solution file.

The `__cwd` built-in variable provides the original working directory path in action expressions.

### Example: Reference a Script in CWD

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cwd-escape-demo
  version: 1.0.0

spec:
  resolvers: {}

  workflow:
    actions:
      # Run a script from the original CWD
      run-script:
        provider: exec
        inputs:
          command: bash
          args:
            expr: '[__cwd + "/scripts/deploy.sh"]'

      # Write a log that records where files came from
      write-log:
        provider: file
        inputs:
          operation: write
          path: build-log.txt
          content:
            expr: >-
              "Build completed\n" +
              "Source directory: " + __cwd + "\n" +
              "Output directory: " + __cwd + "/../output"
```

### Example: Copy a File via Content

```yaml
# Read from CWD in a resolver, write to output-dir in an action
spec:
  resolvers:
    readme:
      type: any
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: README.md

  workflow:
    actions:
      copy-readme:
        provider: file
        inputs:
          operation: write
          path: README.md
          content:
            rslvr: readme
```

Because resolvers always use CWD and actions use `--output-dir`, this effectively copies `README.md` from CWD to the output directory.

---

## Combining with Dry-Run

Use `--dry-run` with `--output-dir` to preview what files would be written and where:

```bash
scafctl run solution -f solution.yaml --output-dir /tmp/demo-output --dry-run
```

This resolves all values and shows the action plan but doesn't create any files or directories. Use this to verify paths before a real run.

---

## Per-Provider Usage

The `--output-dir` flag also works with `run provider` when the capability is `action`:

```bash
# Write a file to the output directory using the file provider directly
scafctl run provider file --capability action --output-dir /tmp/demo-output \
  -i '{"operation": "write", "path": "hello.txt", "content": "world"}'

# Verify
cat /tmp/demo-output/hello.txt
# Output: world
```

```bash
# Create a directory inside the output directory
scafctl run provider directory --capability action --output-dir /tmp/demo-output \
  -i '{"operation": "mkdir", "path": "sub/nested"}'

# Verify
ls -la /tmp/demo-output/sub/nested
```

The flag only applies when `--capability action` is used. Resolver-mode provider runs ignore `--output-dir`:

```bash
# --output-dir is ignored for resolver capability (reads from CWD)
scafctl run provider file --capability resolver --output-dir /tmp/demo-output \
  -i '{"operation": "read", "path": "source.txt"}'
```

---

## Default via Configuration

Instead of passing `--output-dir` every time, set a default in your scafctl configuration:

```yaml
# In scafctl config (e.g., scafctl config set action.outputDir /path/to/output)
action:
  outputDir: /path/to/default/output
```

The CLI flag always overrides the configured default:

```bash
# Uses the configured default
scafctl run solution -f solution.yaml

# Overrides the configured default
scafctl run solution -f solution.yaml --output-dir /tmp/override
```

---

## Absolute Paths Are Never Rewritten

When a provider input uses an absolute path, it is used as-is regardless of `--output-dir`:

```yaml
workflow:
  actions:
    absolute-example:
      provider: file
      inputs:
        operation: write
        path: /etc/myapp/config.yaml    # Absolute — not affected by --output-dir
        content: "some config"

    relative-example:
      provider: file
      inputs:
        operation: write
        path: config.yaml               # Relative — resolves to output-dir
        content: "some config"
```

With `--output-dir /tmp/output`:
- `absolute-example` writes to `/etc/myapp/config.yaml`
- `relative-example` writes to `/tmp/output/config.yaml`

---

## Affected Providers

The following providers use `--output-dir` for path resolution in action mode:

| Provider | Operations Affected |
|----------|-------------------|
| `file` | read, write, exists, delete, write-tree |
| `directory` | list, mkdir, rmdir, copy |
| `exec` | workingDir defaults to output-dir when empty |
| `hcl` | parse, format, validate, list |

Providers like `static`, `cel`, `http`, and `solution` are unaffected because they don't perform filesystem operations.

### Exec Provider Special Behavior

The `exec` provider treats `workingDir` specially:

- **Empty `workingDir`** with `--output-dir` → commands run in the output directory
- **Relative `workingDir`** with `--output-dir` → resolved against the output directory
- **Absolute `workingDir`** → always used as-is

```yaml
workflow:
  actions:
    # Runs in the output directory (workingDir defaults to output-dir)
    list-output:
      provider: exec
      inputs:
        command: ls -la

    # Runs in output-dir/subdir
    list-subdir:
      provider: exec
      inputs:
        command: ls -la
        workingDir: subdir

    # Runs in /tmp regardless of output-dir
    list-tmp:
      provider: exec
      inputs:
        command: ls -la
        workingDir: /tmp
```

---

## Common Patterns

### Generate a Project into a Target Directory

```yaml
spec:
  resolvers:
    config:
      type: any
      resolve:
        with:
          - provider: static
            inputs:
              value:
                name: my-service
                port: 8080

  workflow:
    actions:
      write-dockerfile:
        provider: file
        inputs:
          operation: write
          path: Dockerfile
          content:
            expr: >-
              "FROM golang:1.23\n" +
              "EXPOSE " + string(_.config.port) + "\n" +
              "CMD [\"./\" + _.config.name + \"]"

      write-readme:
        provider: file
        inputs:
          operation: write
          path: README.md
          content:
            expr: '"# " + _.config.name + "\n\nPort: " + string(_.config.port)'

      create-dirs:
        provider: directory
        inputs:
          operation: mkdir
          path: cmd/server
```

```bash
scafctl run solution -f solution.yaml --output-dir ./generated/my-service
```

### CI/CD: Build Artifacts to a Staging Directory

```bash
# In a CI pipeline, write outputs to a staging dir
scafctl run solution -f build.yaml --output-dir "$BUILD_DIR/artifacts"

# Then upload or deploy from that directory
aws s3 sync "$BUILD_DIR/artifacts" s3://my-bucket/
```

### Template Rendering with Source Separation

```yaml
spec:
  resolvers:
    # Read template from CWD
    template:
      type: any
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: templates/deployment.yaml.tpl

    values:
      type: any
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: values.yaml
              format: yaml

  workflow:
    actions:
      # Write rendered output to output-dir
      render:
        provider: file
        inputs:
          operation: write
          path: k8s/deployment.yaml
          content:
            tmpl: "{{ .template }}"
```

```bash
# Templates stay in source, rendered output goes elsewhere
scafctl run solution -f solution.yaml --output-dir ./dist
```

---

## Next Steps

- See the [Output Directory design doc](../../design/output-dir/) for implementation details
- Try the [complete example](../../../examples/solutions/output-dir/) for a working demonstration
- Learn about [Dry-Run](../dryrun-tutorial/) to preview output-dir behavior before executing
- Explore the [Directory Provider Tutorial](../directory-provider-tutorial/) for more filesystem operations

---
title: "Directory Provider Tutorial"
weight: 95
---

# Directory Provider Tutorial

This tutorial walks you through using the `directory` provider to list, scan, create, remove, and copy directories. You'll learn how to use filtering, recursive traversal, content reading, and checksums for filesystem-oriented workflows.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with YAML syntax and solution files
- A local directory with some files to experiment with

## Table of Contents

1. [Listing Directory Contents](#listing-directory-contents)
2. [Recursive Traversal](#recursive-traversal)
3. [Filtering with Globs and Regex](#filtering-with-globs-and-regex)
4. [Reading File Contents](#reading-file-contents)
5. [Computing Checksums](#computing-checksums)
6. [Creating Directories](#creating-directories)
7. [Removing Directories](#removing-directories)
8. [Copying Directories](#copying-directories)
9. [Dry Run Mode](#dry-run-mode)
10. [Common Patterns](#common-patterns)

---

## Listing Directory Contents

The simplest use of the directory provider is listing files and directories in a given path.

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: list-directory
  version: 1.0.0

spec:
  resolvers:
    project-files:
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: .
```

Run it:

```bash
scafctl run solution -f list-dir.yaml
```

The output includes an `entries` array where each entry has:

| Field | Description |
|-------|-------------|
| `name` | File or directory name |
| `path` | Relative path from the listed directory |
| `absolutePath` | Full filesystem path |
| `size` | Size in bytes |
| `isDir` | Whether the entry is a directory |
| `type` | `file` or `dir` |
| `mode` | Permission mode (e.g., `0644`) |
| `modTime` | Last modification time (RFC3339) |
| `extension` | File extension (e.g., `.go`) |
| `mimeType` | MIME type based on extension |

The output also provides summary fields: `totalCount`, `fileCount`, `dirCount`, `totalSize`, and `basePath`.

---

## Recursive Traversal

Enable `recursive: true` to traverse subdirectories. Control the depth with `maxDepth` (default: 10, maximum: 50).

```yaml
spec:
  resolvers:
    all-files:
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: ./src
              recursive: true
              maxDepth: 5
```

> **Tip:** Use `excludeHidden: true` to skip files and directories starting with `.` (e.g., `.git`, `.env`).

---

## Filtering with Globs and Regex

### Glob Filtering

Use `filterGlob` to match entry names using standard glob patterns:

```yaml
spec:
  resolvers:
    go-files:
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: ./pkg
              recursive: true
              filterGlob: "*.go"
```

Common glob patterns:
- `*.go` — all Go files
- `*.yaml` — all YAML files
- `test_*` — files starting with `test_`
- `*.{json,yaml}` — JSON and YAML files (if your shell expands it)

### Regex Filtering

For more complex matching, use `filterRegex`:

```yaml
spec:
  resolvers:
    test-files:
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: ./pkg
              recursive: true
              filterRegex: "_test\\.go$"
```

> **Note:** `filterGlob` and `filterRegex` are mutually exclusive. The provider returns an error if both are specified.

---

## Reading File Contents

Set `includeContent: true` to read file contents into each entry. Text files are returned as plain strings; binary files are base64-encoded.

```yaml
spec:
  resolvers:
    config-contents:
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: ./config
              includeContent: true
              filterGlob: "*.yaml"
              maxFileSize: 524288  # Skip files larger than 512 KB
```

Each entry with content will include:

| Field | Description |
|-------|-------------|
| `content` | The file content (text or base64) |
| `contentEncoding` | `text` for text files, `base64` for binary files |

Files exceeding `maxFileSize` (default: 1 MB) are skipped with a warning.

---

## Computing Checksums

When `includeContent` is true, you can also compute checksums using `md5`, `sha256`, or `sha512`:

```yaml
spec:
  resolvers:
    verified-configs:
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: ./config
              includeContent: true
              checksum: sha256
              filterGlob: "*.yaml"
```

Each entry will include `checksum` and `checksumAlgorithm` fields.

> **Note:** Checksums require `includeContent: true` since the file must be read to compute the hash.

---

## Creating Directories

Use the `mkdir` operation to create directories. Set `createDirs: true` to create the full path (like `mkdir -p`).

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: create-dirs
  version: 1.0.0

spec:
  resolvers: {}
  workflow:
    actions:
      setup-output:
        provider: directory
        inputs:
          operation: mkdir
          path: ./output/reports/2026
          createDirs: true
```

The action returns `{ "success": true, "operation": "mkdir", "path": "<absolute-path>" }`.

Without `createDirs: true`, the parent directory must already exist or the operation fails.

---

## Removing Directories

The `rmdir` operation removes directories. By default it only removes empty directories. Use `force: true` to remove non-empty directories recursively.

```yaml
workflow:
  actions:
    cleanup:
      provider: directory
      inputs:
        operation: rmdir
        path: ./tmp/build-cache
        force: true
```

> **Warning:** `force: true` permanently deletes the directory and all its contents. Use `--dry-run` first to verify what would be removed.

---

## Copying Directories

The `copy` operation recursively copies a directory tree to a new location:

```yaml
workflow:
  actions:
    backup:
      provider: directory
      inputs:
        operation: copy
        path: ./config
        destination: ./config-backup
```

Both `path` (source) and `destination` are required. The source must be an existing directory.

---

## Dry Run Mode

All mutating operations (`mkdir`, `rmdir`, `copy`) support dry-run mode. Use `--dry-run` to see what would happen without making changes:

```bash
scafctl run solution -f solution.yaml --dry-run
```

In dry-run mode, the provider returns the intended operation details without modifying the filesystem. The `list` operation runs normally since it is read-only.

---

## Common Patterns

### Find and Process Config Files

Use the directory provider to discover config files, then process them with other providers:

```yaml
spec:
  resolvers:
    configs:
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: ./config
              recursive: true
              filterGlob: "*.yaml"
              includeContent: true

    config-count:
      type: any
      dependsOn: [configs]
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                {
                  "count": _.configs.fileCount,
                  "names": _.configs.entries.map(e, e.name)
                }
```

### Verify File Integrity

Compute checksums for all files in a directory and compare against known values:

```yaml
spec:
  resolvers:
    checksums:
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: ./dist
              recursive: true
              includeContent: true
              checksum: sha256
              excludeHidden: true

    integrity-check:
      type: any
      dependsOn: [checksums]
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                _.checksums.entries.map(e, {
                  "file": e.name,
                  "sha256": e.checksum
                })
```

### Setup and Teardown with Actions

Combine directory operations in an action workflow:

```yaml
workflow:
  actions:
    create-workspace:
      provider: directory
      inputs:
        operation: mkdir
        path: ./workspace/output
        createDirs: true

    run-build:
      provider: exec
      dependsOn: [create-workspace]
      inputs:
        command: "make build"
        shell: true

    archive-output:
      provider: directory
      dependsOn: [run-build]
      inputs:
        operation: copy
        path: ./workspace/output
        destination: ./archive/latest

  finally:
    cleanup-workspace:
      provider: directory
      inputs:
        operation: rmdir
        path: ./workspace
        force: true
```

---

## Next Steps

- See the [Provider Reference](../provider-reference/) for the complete directory provider input/output schema
- Explore [Resolver Tutorial](../resolver-tutorial/) for combining providers with dependencies and transforms
- Check the [Actions Tutorial](../actions-tutorial/) for building workflows with directory operations

---
title: "File Conflict Strategies"
weight: 18
---

# File Conflict Strategies

## Overview

The file provider currently writes files unconditionally — `os.WriteFile()` silently overwrites any existing content. This creates problems for scaffolding workflows where solutions are run repeatedly during development: generated files may overwrite user edits, there is no feedback about what changed, and no way to control write behavior per-file.

This design introduces a **conflict strategy system** (`onConflict`) to the file provider's `write` and `write-tree` operations. It adds content-based change detection (SHA256 checksums), optional file backups, an append mode with line-level deduplication, and detailed per-file status reporting.

## Problem Statement

1. **Silent overwrites** — Running a solution twice replaces all output files with no warning, even if the user manually edited them.
2. **No idempotency** — Identical re-runs produce identical files but still perform unnecessary disk writes.
3. **No append support** — Common scaffolding patterns (e.g., adding entries to `.gitignore`) require appending to existing files, not replacing them.
4. **No visibility** — Output reports `success: true` with no indication of whether a file was newly created, unchanged, or overwritten.

## Design

### Conflict Strategies

A new `onConflict` input controls behavior when the target file already exists:

| Strategy | Behavior | Use Case |
|----------|----------|----------|
| `skip-unchanged` | SHA256 compare existing vs new content. Skip if identical; overwrite if different. | **Default.** Idempotent re-generation. |
| `overwrite` | Always replace the entire file. | Forced regeneration, CI pipelines. |
| `skip` | Never write if file exists. | One-time scaffolding (initial project setup). |
| `error` | Return a Go error if file exists. For `write`, this is an immediate error return. For `write-tree`, collects all conflicts and reports them in a single error by default (see `failFast`). | Strict safety, preventing accidental overwrites. |
| `append` | Append content to the end of the file. Create if missing. | Log files, accumulating output. |

When the file does **not** exist, all strategies behave identically: the file is created.

### Append with Deduplication

The `append` strategy supports an optional `dedupe` boolean input:

| `dedupe` | Behavior |
|----------|----------|
| `false` (default) | Raw append — content is concatenated to the end of the file unconditionally. |
| `true` | Line-level deduplication — only lines not already present in the existing file are appended. If all lines already exist, the file is left unchanged. |

**Line splitting semantics:**

- Lines are split on `\n`. Trailing `\r` is stripped from each line before comparison, so both `\n` and `\r\n` line endings are handled correctly across platforms.
- Comparison is **case-sensitive** and **whitespace-significant** (after `\r` stripping). No trimming is performed.
- Empty lines participate in deduplication — an empty string in the existing file matches an empty string in the new content.
- If the existing file does not end with a `\n`, a `\n` separator is inserted before appending new content to avoid concatenating with the last existing line.

This is designed for files like `.gitignore`, `.dockerignore`, or any line-oriented configuration where duplicate entries are undesirable.

**Empty content:** If the `content` to append is empty (zero bytes), the file is left unchanged and the status is `unchanged`. No separator is inserted and the file is not created if it does not exist. When `dedupe: true`, empty content also results in `unchanged` since there are no new lines to append.

**Trailing newline in appended content:** The provider does **not** normalize the appended content. If the user provides content without a trailing `\n` (e.g., `"build/"` instead of `"build/\n"`), the resulting file will not end with a newline. Users are responsible for providing properly terminated content for line-oriented files.

**Example — `.gitignore` management:**

```yaml
# Existing .gitignore contains: dist/, .env, node_modules/
# New content to append:
provider: file
inputs:
  operation: write
  path: .gitignore
  content: |
    .env
    build/
    node_modules/
  onConflict: append
  dedupe: true

# Result: .env and node_modules/ are already present, only build/ is appended.
# Final .gitignore: dist/, .env, node_modules/, build/
```

The `dedupe` flag is only valid when `onConflict: append`. Setting `dedupe: true` with any other strategy (e.g., `overwrite`, `skip`) returns a **validation error**: `"dedupe is only valid when onConflict is append"`. This applies at all levels — invocation-level and per-entry.

### Optional Backup

A `backup` boolean input (default `false`) creates a `.bak` copy of the existing file before any mutation occurs. This applies to:

- `overwrite` — always backs up before replacing.
- `skip-unchanged` — backs up before overwriting when content differs (not when content matches).
- `append` — backs up before appending new content (not when all lines are duplicates and status is `unchanged`).

The backup preserves the original file's permissions. The implementation reads the source file's mode via `os.Stat` and applies it to the backup copy via `os.Chmod`.

Backup naming follows a numbered scheme: `.bak`, `.bak.1`, `.bak.2`, etc., capped at a configurable limit defined in `pkg/settings/settings.go` (`DefaultMaxBackups`). When the backup cap is reached, `backupFile` returns an error and the write operation fails rather than silently losing data. Error message: `"backup limit reached for <path>: maximum <N> backups"`.

### Content Comparison

Change detection for `skip-unchanged` uses SHA256 checksums:

1. Compute SHA256 of the **new content** (in-memory, from the `content` input).
2. Compute SHA256 of the **existing file** (streaming read via `io.Copy` into `sha256.New()`).
3. Compare digests.

This is stateless — no manifest or state file is required. The comparison is efficient for files of any size since it streams the existing file rather than loading it entirely into memory.

> **Note — TOCTOU window:** The `skip-unchanged` strategy performs a stat → SHA256 compare → write sequence. Between the comparison and the write, another process (or a parallel action in the same solution run) could modify the file. File-level atomicity is not guaranteed. This is acceptable for a scaffolding CLI where concurrent writes to the same file are an anti-pattern. Atomic writes (write-to-temp + rename) may be added as a future optimization if needed.
>
> The same TOCTOU window applies to the `error` strategy's pre-scan in `write-tree`: the check-all phase runs `os.Stat` on all entries before any writes begin. A file could be created between the scan and the write loop. This is acceptable for the same reasons.

### Strategy Precedence

The conflict strategy is configurable at three layers, with the most specific layer winning:

```
per-entry (write-tree only)  >  per-invocation (provider input)  >  CLI flag  >  default
```

| Layer | Scope | Configuration |
|-------|-------|---------------|
| **Per-entry** | Individual file in `write-tree` entries array | `entries[].onConflict`, `entries[].dedupe`, `entries[].backup` |
| **Per-invocation** | All files in a single provider call | `inputs.onConflict`, `inputs.dedupe`, `inputs.backup` |
| **CLI flag** | All file writes in a solution run | `--on-conflict <strategy>` on the `run` command |
| **Default** | Hardcoded fallback | `skip-unchanged` (defined in `pkg/settings/settings.go`) |

Resolution logic:
1. If the entry specifies `onConflict`, use it.
2. Else if the provider invocation specifies `onConflict`, use it.
3. Else if the CLI flag `--on-conflict` is set, use it (injected via context).
4. Else use the default (`skip-unchanged`).

> **Note:** The `--on-conflict` CLI flag is the **lowest-priority** override and only acts as a default for file provider actions that do not specify their own `onConflict`. Provider-level and entry-level settings always take precedence. Be aware that `--on-conflict error`, for example, applies to *every* file provider action in the solution unless explicitly overridden at the provider or entry level.

### Per-File Status Reporting

#### `write` Operation

The output gains a `status` field indicating the outcome:

```json
{
  "success": true,
  "path": "/abs/path/to/file",
  "status": "created",
  "backupPath": "/abs/path/to/file.bak"
}
```

#### `write-tree` Operation

The output gains a `filesStatus` array and summary counts:

```json
{
  "success": true,
  "operation": "write-tree",
  "basePath": "/abs/output",
  "filesWritten": 3,
  "paths": ["src/main.go", "config.yaml", ".gitignore"],
  "filesStatus": [
    { "path": "src/main.go", "status": "created" },
    { "path": "config.yaml", "status": "overwritten", "backupPath": "/abs/output/config.yaml.bak" },
    { "path": ".gitignore", "status": "appended" },
    { "path": "README.md", "status": "skipped" },
    { "path": "go.mod", "status": "unchanged" }
  ],
  "created": 1,
  "overwritten": 1,
  "appended": 1,
  "skipped": 1,
  "unchanged": 1
}
```

Existing output fields (`success`, `path`, `paths`, `basePath`, `operation`) are preserved. **Breaking change:** `filesWritten` changes semantics from `len(entries)` (all files in the entries array) to `created + overwritten + appended` (only files that were actually written to disk). This is an intentional breaking change per project conventions — any code or solution tests asserting `filesWritten == len(entries)` must be updated.

> **Note on `backupPath` placement:** For the `write` operation, `backupPath` is a top-level output field (since there is only one file). For `write-tree`, `backupPath` is nested inside each `filesStatus[]` entry (since there are multiple files). This is intentional and consistent with the single-vs-many pattern.

### Status Values

| Status | Meaning |
|--------|---------|
| `created` | File did not exist; new file written. |
| `overwritten` | File existed; replaced with new content. |
| `skipped` | File existed; left untouched (skip or error-avoidance). |
| `unchanged` | File existed; content identical, no write performed. |
| `appended` | File existed; content appended (with or without dedup). |

### Dry-Run Behavior

In dry-run mode, the provider checks whether each target file exists and reports what **would** happen without performing any writes:

```json
{
  "_dryRun": true,
  "_plannedStatus": "overwritten",
  "_strategy": "skip-unchanged",
  "_message": "Would overwrite /abs/path/to/file (1024 bytes)",
  "path": "/abs/path/to/file"
}
```

The `_strategy` field shows the resolved conflict strategy that was used to compute the planned status. When `backup: true` is set, a `_backup: true` field is also included.

For `write-tree`, each entry includes a `_plannedStatus` and `_strategy` showing the intended action based on the current filesystem state and the effective conflict strategy.

> **Note — cost of `skip-unchanged` in dry-run:** When the effective strategy is `skip-unchanged`, dry-run mode performs a full SHA256 comparison by reading each existing file. For large `write-tree` operations with many files, this may be slower than other strategies. This is acceptable because dry-run is an explicit opt-in and the cost is I/O-bound, not compute-bound.

## Schema Changes

### New Input Fields

| Field | Type | Default | Applies To | Description |
|-------|------|---------|------------|-------------|
| `onConflict` | `string` | `"skip-unchanged"` | `write`, `write-tree` | Conflict resolution strategy. One of: `error`, `overwrite`, `skip`, `skip-unchanged`, `append`. |
| `backup` | `bool` | `false` | `write`, `write-tree` | Create `.bak` backup before mutating existing files (applies to `overwrite`, `skip-unchanged` when content differs, and `append` when content is appended). |
| `dedupe` | `bool` | `false` | `write`, `write-tree` | When `onConflict: append`, perform line-level deduplication. **Validation error** if set to `true` with any other strategy. |
| `failFast` | `bool` | `false` | `write-tree` | When `onConflict: error`, stop at the first conflicting file instead of collecting all conflicts. Only applies to `write-tree`. Has no effect on other strategies or other error conditions (e.g., permission errors, backup cap exceeded). |

### Per-Entry Overrides (write-tree)

The `entries` array items gain optional fields:

| Field | Type | Description |
|-------|------|-------------|
| `onConflict` | `string` | Override the invocation-level conflict strategy for this entry. |
| `dedupe` | `bool` | Override the invocation-level dedupe flag for this entry. |
| `backup` | `bool` | Override the invocation-level backup flag for this entry. |

### CLI Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--on-conflict` | `string` | (none) | Default conflict strategy for all file writes in this run. This is the **lowest-priority** layer — overridden by provider-level and entry-level settings. |
| `--backup` | `bool` | `false` | Create `.bak` backups before mutating existing files. This is the **lowest-priority** layer — overridden by provider-level and entry-level `backup` settings. |

## Architecture

### New Types (`pkg/provider/builtin/fileprovider/conflict.go`)

```go
// ConflictStrategy controls behavior when a target file already exists.
type ConflictStrategy string

const (
    ConflictError         ConflictStrategy = "error"
    ConflictOverwrite     ConflictStrategy = "overwrite"
    ConflictSkip          ConflictStrategy = "skip"
    ConflictSkipUnchanged ConflictStrategy = "skip-unchanged"
    ConflictAppend        ConflictStrategy = "append"
)

// FileWriteStatus indicates the outcome of a file write operation.
type FileWriteStatus string

const (
    StatusCreated     FileWriteStatus = "created"
    StatusOverwritten FileWriteStatus = "overwritten"
    StatusSkipped     FileWriteStatus = "skipped"
    StatusUnchanged   FileWriteStatus = "unchanged"
    StatusAppended    FileWriteStatus = "appended"
)
```

### Helper Functions (`pkg/provider/builtin/fileprovider/conflict.go`)

| Function | Signature | Purpose |
|----------|-----------|---------|
| `contentMatchesFile` | `(absPath string, newContent []byte) (bool, error)` | SHA256 comparison of new content vs existing file. Returns `(false, nil)` when the file does not exist (`os.ErrNotExist`), so callers can safely call it without a prior stat check. Other I/O errors are returned as `(false, err)`. |
| `backupFile` | `(absPath string) (string, error)` | Copy existing file to `.bak` with numbered fallback. |
| `appendToFile` | `(absPath string, content []byte, fileMode os.FileMode, dedupe bool) (FileWriteStatus, error)` | Append content with optional line-level deduplication. |

### Context Injection (`pkg/provider/context.go`)

```go
func WithConflictStrategy(ctx context.Context, strategy string) context.Context
func ConflictStrategyFromContext(ctx context.Context) (string, bool)

func WithBackup(ctx context.Context, backup bool) context.Context
func BackupFromContext(ctx context.Context) (bool, bool)
```

The CLI `--on-conflict` and `--backup` flag values are injected into the context before solution execution. The file provider reads them as fallbacks when no explicit `onConflict` or `backup` input is provided.

### Debug Logging

When verbose/debug logging is enabled, the file provider logs the **resolved conflict strategy** and **resolved backup flag** for each file write. This helps users understand which precedence layer took effect. The resolved values are logged at debug level — they are not included in the structured output schema.

## Decision Log

| Decision | Rationale |
|----------|-----------|
| **Default = `skip-unchanged`** | Idempotent and developer-friendly. Skips identical files, overwrites only when content differs. Breaking change from current silent-overwrite (allowed per project conventions). |
| **SHA256 content comparison, no state tracking** | Stateless and efficient. No manifest file to manage. SHA256 is fast and collision-resistant for this use case. |
| **Backup naming: `.bak`, `.bak.1`, `.bak.2`** | Simple numbered scheme. Capped at a configurable limit in `pkg/settings/settings.go`. |
| **Three-layer precedence** | Follows existing patterns (e.g., `permissions` at invocation level). Per-entry override is essential for mixed write-tree scenarios. |
| **`append` with `dedupe` flag** | Raw append covers accumulation use cases. `dedupe: true` covers line-oriented config file management (`.gitignore`, `.dockerignore`). Keeping them as one strategy with a flag avoids enum bloat. |
| **Structured merge excluded** | YAML/JSON deep-merge involves complex semantics (conflict resolution, array handling, format detection). Users can achieve merge via CEL/go-template providers (read → merge → write). Marker-based insertion (e.g., appending after a `// GENERATED — DO NOT EDIT BELOW` marker) is a simpler future candidate that doesn't require format detection. Both may be added as future strategies. |
| **`exec` provider out of scope** | The exec provider delegates to shell commands and cannot inspect their file writes. Conflict awareness only applies to the file provider where we control the writes. |
| **No atomic write guarantee** | The `skip-unchanged` strategy has a TOCTOU window between content comparison and file write. File-level atomicity is not guaranteed. Acceptable for a scaffolding CLI where concurrent writes to the same file are an anti-pattern. Atomic writes (write-to-temp + rename) may be added as a future optimization. |
| **`filesWritten` semantic change (breaking)** | `filesWritten` changes from counting all entries to counting only files actually written to disk (`created + overwritten + appended`). Intentional breaking change — the old behavior was misleading when files were skipped or unchanged. |
| **Backup preserves permissions** | Backup copies retain the original file's permission mode via `os.Stat` + `os.Chmod`. Prevents security-sensitive files (e.g., `0600`) from gaining overly permissive backup copies. |
| **Backup cap returns error** | When `DefaultMaxBackups` is reached, the write operation fails with an error rather than silently losing data or overwriting the oldest backup. Loud failure is preferred over silent data loss. |
| **`dedupe` validation on non-append** | Setting `dedupe: true` on a non-`append` strategy returns a validation error. This catches user misconfigurations immediately rather than silently ignoring the flag. |
| **`error` strategy: check-all by default** | In `write-tree`, the `error` strategy collects all conflicting file paths and reports them in a single error. This is more user-friendly than fail-fast. The `failFast` input allows opting into stop-at-first behavior when needed. |
| **`error` strategy returns Go error** | The `error` strategy returns a Go `error` (not a `success: false` structured output). For `write`, this is an immediate `fmt.Errorf`. For `write-tree` with check-all, this is a single error listing all conflicting paths. This is consistent with how other provider errors are returned. |
| **Per-entry `backup` override** | `backup` is configurable per-entry in `write-tree`, consistent with `onConflict` and `dedupe`. A mixed `write-tree` may need backups on overwritten files but not on appended config files. |
| **`--backup` CLI flag** | A `--backup` CLI flag mirrors `--on-conflict` for consistency. CI workflows that always want backups can set it once instead of adding `backup: true` to every solution file. Same lowest-priority precedence: entry > invocation > CLI flag > default. |
| **`contentMatchesFile` tolerates missing files** | Returns `(false, nil)` for `os.ErrNotExist` instead of requiring callers to stat first. Simplifies call sites and avoids redundant stat calls. |
| **Empty content on append returns `unchanged`** | Appending zero bytes is a no-op. Returning `unchanged` (rather than `appended` with no effect) gives accurate status reporting. The file is not created if it does not exist. |
| **Debug logging of resolved strategy** | The resolved conflict strategy and backup flag are logged at debug level for each file write. This aids troubleshooting without polluting the structured output schema. |

## Examples

### Idempotent regeneration (default behavior)

```yaml
workflow:
  actions:
    generate-config:
      provider: file
      inputs:
        operation: write-tree
        basePath: ./output
        entries: { rslvr: rendered }
        # onConflict defaults to skip-unchanged
        # Second run: unchanged files are skipped, modified templates are overwritten
```

### One-time scaffolding with backup

```yaml
workflow:
  actions:
    scaffold-project:
      provider: file
      inputs:
        operation: write-tree
        basePath: .
        entries: { rslvr: scaffoldFiles }
        onConflict: skip
        # Existing files are never overwritten — safe for initial project setup
```

### Force overwrite with backup in CI

```yaml
workflow:
  actions:
    ci-generate:
      provider: file
      inputs:
        operation: write-tree
        basePath: ./generated
        entries: { rslvr: rendered }
        onConflict: overwrite
        backup: true
        # All files replaced, originals saved as .bak
```

### Mixed strategies with write-tree per-entry overrides

```yaml
workflow:
  actions:
    write-files:
      provider: file
      inputs:
        operation: write-tree
        basePath: .
        entries:
          - path: src/main.go
            content: { rslvr: mainGoContent }
            # Inherits invocation-level onConflict (default: skip-unchanged)
          - path: .gitignore
            content: "dist/\nbuild/\nnode_modules/\n"
            onConflict: append
            dedupe: true
            # Only unique lines are appended
          - path: LICENSE
            content: { rslvr: licenseContent }
            onConflict: skip
            # Never overwrite an existing LICENSE file
          - path: config.yaml
            content: { rslvr: configContent }
            onConflict: overwrite
            backup: true
            # Always overwrite config, but keep a backup of the original
```

### CLI override for all file writes

```bash
# Error on any existing files (strict mode)
# Note: --on-conflict is the lowest-priority default.
# Any action with an explicit onConflict input overrides this.
scafctl run solution -f solution.yaml --on-conflict error

# Force overwrite everything
scafctl run solution -f solution.yaml --on-conflict overwrite

# Force overwrite with backups
scafctl run solution -f solution.yaml --on-conflict overwrite --backup

# Append mode for all writes
scafctl run solution -f solution.yaml --on-conflict append
```

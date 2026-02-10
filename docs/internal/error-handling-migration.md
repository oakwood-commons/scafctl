# Error Handling Migration Plan

## Overview

All CLI commands need to follow the error handling pattern where:
1. Commands print styled errors via `writer.FromContext(ctx).Errorf()`
2. Commands return `exitcode.WithCode(err, exitcode.XxxError)`
3. `main.go` only extracts exit code via `exitcode.GetCode(err)` and exits

## Already Updated Commands ✅

The following commands already use the proper pattern:

| Command | File |
|---------|------|
| `catalog delete` | `pkg/cmd/scafctl/catalog/delete.go` |
| `catalog inspect` | `pkg/cmd/scafctl/catalog/inspect.go` |
| `catalog list` | `pkg/cmd/scafctl/catalog/list.go` |
| `catalog prune` | `pkg/cmd/scafctl/catalog/prune.go` |
| `build solution` | `pkg/cmd/scafctl/build/solution.go` |
| `run solution` | `pkg/cmd/scafctl/run/solution.go` |
| `run common` | `pkg/cmd/scafctl/run/common.go` |
| `get solution` | `pkg/cmd/scafctl/get/solution/solution.go` |
| `lint` | `pkg/cmd/scafctl/lint/lint.go` |

## Commands Requiring Updates

### Exit Codes Reference

```go
const (
    Success          = 0
    GeneralError     = 1
    ValidationFailed = 2
    InvalidInput     = 3
    FileNotFound     = 4
    RenderFailed     = 5
    ActionFailed     = 6
    ConfigError      = 7
    CatalogError     = 8
)
```

---

### 1. `version` Command

**File**: `pkg/cmd/scafctl/version/version.go`

**Error returns to update** (3 total):
- Line 58: `return err`
- Line 64: `return err`
- Line 108: `return err`

**Pattern**:
```go
// Before
return err

// After
w := writer.FromContext(ctx)
w.Errorf("%v", err)
return exitcode.WithCode(err, exitcode.GeneralError)
```

**Required imports**:
```go
"github.com/oakwood-commons/scafctl/pkg/exitcode"
"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
```

---

### 2. `get resolver refs` Command

**File**: `pkg/cmd/scafctl/get/resolver/refs.go`

**Error returns to update** (8 total):
- Line 120: `return fmt.Errorf("one of --template-file, --template, or --expr is required")`
- Line 123: `return fmt.Errorf("only one of --template-file, --template, or --expr can be specified")`
- Line 142: `return err`
- Line 154: `return err`
- Line 162: `return err`
- Line 282: `return fmt.Errorf("unknown output format: %s (supported: text, json, yaml)", format)`

**Recommended exit codes**:
- Lines 120, 123, 282: `exitcode.InvalidInput`
- Lines 142, 154, 162: `exitcode.GeneralError`

---

### 3. `get provider` Command

**File**: `pkg/cmd/scafctl/get/provider/provider.go`

**Error returns to update** (2 total):
- Line 132: `return fmt.Errorf("interactive mode requires a terminal")` → `exitcode.InvalidInput`
- Line 166: `return fmt.Errorf("provider %q not found", name)` → `exitcode.FileNotFound`

---

### 4. `resolver graph` Command

**File**: `pkg/cmd/scafctl/resolver/graph.go`

**Error returns to update** (9 total):
- Line 86: `return fmt.Errorf("failed to read config file: %w", err)` → `exitcode.FileNotFound`
- Line 95: `return fmt.Errorf("failed to parse config file: %w", err)` → `exitcode.InvalidInput`
- Line 99: `return fmt.Errorf("no resolvers found in config file")` → `exitcode.InvalidInput`
- Line 107: `return fmt.Errorf("failed to build dependency graph: %w", err)` → `exitcode.GeneralError`
- Lines 119, 124, 129: Render failures → `exitcode.RenderFailed`
- Line 136: JSON encode failure → `exitcode.GeneralError`
- Line 140: Unsupported format → `exitcode.InvalidInput`

---

### 5. `secrets` Commands

**Files**:
- `pkg/cmd/scafctl/secrets/delete.go` (6 error returns)
- `pkg/cmd/scafctl/secrets/set.go` (10 error returns)
- `pkg/cmd/scafctl/secrets/get.go` (9 error returns)
- `pkg/cmd/scafctl/secrets/list.go` (3 error returns)
- `pkg/cmd/scafctl/secrets/exists.go` (4 error returns)
- `pkg/cmd/scafctl/secrets/rotate.go` (5 error returns)
- `pkg/cmd/scafctl/secrets/export.go` (12 error returns)
- `pkg/cmd/scafctl/secrets/import.go` (7 error returns)
- `pkg/cmd/scafctl/secrets/validation.go` (1 error return)

**Total**: ~57 error returns

**Recommended exit codes**:
- Validation/name errors: `exitcode.InvalidInput`
- Store initialization failures: `exitcode.ConfigError`
- Secret not found: `exitcode.FileNotFound`
- I/O errors: `exitcode.GeneralError`

---

### 6. `auth` Commands

**Files**:
- `pkg/cmd/scafctl/auth/login.go` (7 error returns)
- `pkg/cmd/scafctl/auth/logout.go` (4 error returns)
- `pkg/cmd/scafctl/auth/status.go` (2 error returns)
- `pkg/cmd/scafctl/auth/token.go` (4 error returns)

**Total**: ~17 error returns

**Recommended exit codes**:
- Unknown handler: `exitcode.InvalidInput`
- Auth failures: `exitcode.GeneralError`
- Missing required flags: `exitcode.InvalidInput`

---

### 7. `config` Commands

**Files**:
- `pkg/cmd/scafctl/config/add_catalog.go` (6 error returns)
- `pkg/cmd/scafctl/config/remove_catalog.go` (4 error returns)
- `pkg/cmd/scafctl/config/use_catalog.go` (4 error returns)
- `pkg/cmd/scafctl/config/init.go` (5 error returns)
- `pkg/cmd/scafctl/config/set.go` (2 error returns)
- `pkg/cmd/scafctl/config/get.go` (2 error returns)
- `pkg/cmd/scafctl/config/unset.go` (3 error returns)
- `pkg/cmd/scafctl/config/show.go` (3 error returns)
- `pkg/cmd/scafctl/config/schema.go` (1 error return)
- `pkg/cmd/scafctl/config/validate.go` (4 error returns)
- `pkg/cmd/scafctl/config/paths.go` (1 error return)
- `pkg/cmd/scafctl/config/view.go` (1 error return)

**Total**: ~36 error returns

**Recommended exit codes**:
- Invalid input/type: `exitcode.InvalidInput`
- File not found: `exitcode.FileNotFound`
- Config errors: `exitcode.ConfigError`
- Validation errors: `exitcode.ValidationFailed`

---

### 8. `explain` Commands

**Files**:
- `pkg/cmd/scafctl/explain/schema.go` (2 error returns)
- `pkg/cmd/scafctl/explain/provider.go` (1 error return)
- `pkg/cmd/scafctl/explain/solution.go` (1 error return)

**Total**: ~4 error returns

**Recommended exit codes**:
- Unknown kind/provider: `exitcode.FileNotFound`
- Field not found: `exitcode.InvalidInput`
- Load failure: `exitcode.FileNotFound`

---

### 9. `render solution` Command

**File**: `pkg/cmd/scafctl/render/solution.go`

**Error returns to update** (5 total):
- Lines 167, 184, 191, 199, 708: Various errors

**Recommended exit codes**:
- Parse errors: `exitcode.InvalidInput`
- Render errors: `exitcode.RenderFailed`

---

### 10. `snapshot` Commands

**Files**:
- `pkg/cmd/scafctl/snapshot/save.go` (6 error returns)
- `pkg/cmd/scafctl/snapshot/show.go` (3 error returns)
- `pkg/cmd/scafctl/snapshot/diff.go` (7 error returns)

**Total**: ~16 error returns

**Recommended exit codes**:
- File errors: `exitcode.FileNotFound`
- Parse errors: `exitcode.InvalidInput`
- Output format errors: `exitcode.InvalidInput`

---

## Summary

| Category | Files | Error Returns |
|----------|-------|---------------|
| version | 1 | 3 |
| get resolver refs | 1 | 8 |
| get provider | 2 | 2 |
| resolver graph | 1 | 9 |
| secrets | 9 | ~57 |
| auth | 4 | ~17 |
| config | 12 | ~36 |
| explain | 3 | 4 |
| render | 1 | 5 |
| snapshot | 3 | ~16 |
| **Total** | **37 files** | **~157 error returns** |

---

## Implementation Pattern

For each command, apply this pattern:

### Step 1: Add imports
```go
import (
    "github.com/oakwood-commons/scafctl/pkg/exitcode"
    "github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)
```

### Step 2: Get writer from context
At the start of the `Run` function (or in `RunE` closure):
```go
w := writer.FromContext(ctx)
// or
w := writer.MustFromContext(ctx)
```

### Step 3: Replace error returns
```go
// Before
if err != nil {
    return fmt.Errorf("failed to do X: %w", err)
}

// After
if err != nil {
    err := fmt.Errorf("failed to do X: %w", err)
    w.Errorf("%v", err)
    return exitcode.WithCode(err, exitcode.GeneralError)
}
```

### Step 4: For commands without context access
If the command doesn't have direct context access in the error path, create a helper:
```go
func exitWithError(w *writer.Writer, err error, code int) error {
    if w != nil {
        w.Errorf("%v", err)
    }
    return exitcode.WithCode(err, code)
}
```

---

## Testing

After each file update:
1. Run `go build ./...` to verify compilation
2. Run `go test ./pkg/cmd/scafctl/...` to verify tests pass
3. Run `golangci-lint run --fix` to verify linting

After all updates:
1. Run full test suite: `go test ./...`
2. Manual testing of key error paths
3. Verify `--no-color` flag is respected for error output

---

## Notes

- The `get/provider/tui.go` file has error returns but these are internal TUI functions, not command-level returns
- Some `return err` statements may be in utility functions that don't have access to writer - these should bubble up to command level
- Commands that already have `w := writer.FromContext(ctx)` just need the exitcode wrapping added

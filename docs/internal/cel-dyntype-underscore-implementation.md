# CEL `_` Variable DynType Implementation Plan

## Overview

This document outlines the implementation plan to change the CEL `_` variable from `MapType(StringType, DynType)` to `DynType`, enabling consistent behavior across all CEL evaluation contexts.

## Problem Statement

Currently, there's inconsistent behavior when using the `-e` (expression) flag with CLI commands:

```bash
# Without -e: Returns raw array
scafctl auth status -o json
# Output: [{"authenticated": true, ...}]

# With -e _: Returns wrapped object
scafctl auth status -e _ -o json
# Output: {"value": [{"authenticated": true, ...}]}
```

### Root Cause

Two files define the `_` variable with different types:

| File | Type | Purpose |
|------|------|---------|
| `pkg/celexp/context.go:77` | `MapType(StringType, DynType)` | Core CEL evaluation |
| `pkg/terminal/kvx/cel.go:51` | `DynType` | TUI CEL provider |

The kvx CEL evaluation (`EvaluateWithScafctlCEL`) wraps non-map data in `{"value": data}` to satisfy the `MapType` requirement of `celexp.EvaluateExpression`.

## Solution

Change `_` to be `DynType` everywhere, removing the artificial `MapType` constraint. CEL handles type errors at runtime anyway, so the compile-time map constraint provides no real safety benefit.

## Implementation Steps

### Phase 1: Core Changes

#### 1.1 Update `pkg/celexp/context.go`

**File**: `pkg/celexp/context.go`

**Change `BuildCELContext` function signature and implementation:**

```go
// Before
func BuildCELContext(
    resolverData map[string]any,
    additionalVars map[string]any,
) (envOpts []cel.EnvOption, vars map[string]any)

// After
func BuildCELContext(
    rootData any,
    additionalVars map[string]any,
) (envOpts []cel.EnvOption, vars map[string]any)
```

**Change the variable declaration:**

```go
// Before
if resolverData != nil {
    vars["_"] = resolverData
    envOpts = append(envOpts, cel.Variable("_", cel.MapType(cel.StringType, cel.DynType)))
}

// After
if rootData != nil {
    vars["_"] = rootData
    envOpts = append(envOpts, cel.Variable("_", cel.DynType))
}
```

**Update `EvaluateExpression` function signature:**

```go
// Before
func EvaluateExpression(
    ctx context.Context,
    exprStr string,
    resolverData map[string]any,
    additionalVars map[string]any,
    opts ...Option,
) (any, error)

// After
func EvaluateExpression(
    ctx context.Context,
    exprStr string,
    rootData any,
    additionalVars map[string]any,
    opts ...Option,
) (any, error)
```

**Update docstrings** to reflect that `rootData` can be any type, not just a map.

#### 1.2 Update `pkg/terminal/kvx/cel.go`

**File**: `pkg/terminal/kvx/cel.go`

**Remove the wrapping logic in `EvaluateWithScafctlCEL`:**

```go
// Before
func EvaluateWithScafctlCEL(ctx context.Context, expr string, root any) (any, error) {
    var resolverData map[string]any
    if m, ok := root.(map[string]any); ok {
        resolverData = m
    } else {
        resolverData = map[string]any{"value": root}
    }
    result, err := celexp.EvaluateExpression(ctx, expr, resolverData, nil)
    // ...
}

// After
func EvaluateWithScafctlCEL(ctx context.Context, expr string, root any) (any, error) {
    result, err := celexp.EvaluateExpression(ctx, expr, root, nil)
    // ...
}
```

### Phase 2: Update Callers

All callers of `celexp.EvaluateExpression` currently pass `map[string]any` as the second argument. Since `map[string]any` satisfies `any`, **no changes are required** for existing callers. However, verify each caller for correctness:

| File | Line | Status |
|------|------|--------|
| `pkg/action/deferred.go` | 44 | ✅ Already passes `map[string]any` |
| `pkg/resolver/executor.go` | 677, 710, 1107 | ✅ Already passes `map[string]any` |
| `pkg/spec/condition.go` | 30 | ✅ Already passes `map[string]any` |
| `pkg/provider/builtin/celprovider/cel.go` | 181 | ✅ Already passes `map[string]any` |
| `pkg/provider/builtin/debugprovider/debug.go` | 260 | ✅ Already passes `map[string]any` |
| `pkg/provider/builtin/validationprovider/validation.go` | 260 | ✅ Already passes `map[string]any` |
| `pkg/spec/valueref.go` | 137 | ✅ Already passes `map[string]any` |
| `pkg/terminal/kvx/cel.go` | 32 | 🔄 **Will be updated in Phase 1** |

### Phase 3: Update Tests

#### 3.1 Update `pkg/celexp/context_test.go`

Most tests use `map[string]any` and will continue to work. Add new tests for non-map root data:

```go
func TestBuildCELContext_NonMapRootData(t *testing.T) {
    // Test with slice
    t.Run("slice root data", func(t *testing.T) {
        rootData := []any{"a", "b", "c"}
        envOpts, vars := BuildCELContext(rootData, nil)
        
        require.Contains(t, vars, "_")
        assert.Equal(t, rootData, vars["_"])
        
        expr := Expression("size(_)")
        compiled, err := expr.Compile(envOpts)
        require.NoError(t, err)
        
        result, err := compiled.Eval(vars)
        require.NoError(t, err)
        assert.Equal(t, int64(3), result)
    })
    
    // Test with string
    t.Run("string root data", func(t *testing.T) {
        rootData := "hello world"
        envOpts, vars := BuildCELContext(rootData, nil)
        
        require.Contains(t, vars, "_")
        assert.Equal(t, rootData, vars["_"])
        
        expr := Expression("_.upperAscii()")
        compiled, err := expr.Compile(envOpts)
        require.NoError(t, err)
        
        result, err := compiled.Eval(vars)
        require.NoError(t, err)
        assert.Equal(t, "HELLO WORLD", result)
    })
    
    // Test with int
    t.Run("int root data", func(t *testing.T) {
        rootData := int64(42)
        envOpts, vars := BuildCELContext(rootData, nil)
        
        require.Contains(t, vars, "_")
        
        expr := Expression("_ * 2")
        compiled, err := expr.Compile(envOpts)
        require.NoError(t, err)
        
        result, err := compiled.Eval(vars)
        require.NoError(t, err)
        assert.Equal(t, int64(84), result)
    })
}

func TestEvaluateExpression_NonMapRootData(t *testing.T) {
    ctx := context.Background()
    
    t.Run("slice", func(t *testing.T) {
        data := []any{1, 2, 3}
        result, err := EvaluateExpression(ctx, "_", data, nil)
        require.NoError(t, err)
        assert.Equal(t, data, result)
    })
    
    t.Run("identity expression returns raw value", func(t *testing.T) {
        data := []map[string]any{{"name": "test"}}
        result, err := EvaluateExpression(ctx, "_", data, nil)
        require.NoError(t, err)
        assert.Equal(t, data, result)
    })
}
```

#### 3.2 Update `pkg/terminal/kvx/cel_test.go` (if exists)

Add/update tests to verify non-wrapped behavior:

```go
func TestEvaluateWithScafctlCEL_SliceNotWrapped(t *testing.T) {
    ctx := context.Background()
    data := []map[string]any{
        {"name": "item1"},
        {"name": "item2"},
    }
    
    result, err := EvaluateWithScafctlCEL(ctx, "_", data)
    require.NoError(t, err)
    
    // Should return slice directly, not wrapped in {"value": ...}
    resultSlice, ok := result.([]any)
    require.True(t, ok, "expected slice, got %T", result)
    assert.Len(t, resultSlice, 2)
}
```

### Phase 4: Documentation Updates

#### 4.1 Update `pkg/celexp/context.go` docstrings

Update the `BuildCELContext` and `EvaluateExpression` function documentation to reflect that `rootData` can be any type.

#### 4.2 Update `docs/design/cel-integration.md`

Update any references to `_` being a map type.

## Verification

### Manual Testing

```bash
# Build
go build ./...

# Run all tests
go test ./...

# Specific package tests
go test ./pkg/celexp/... -v
go test ./pkg/terminal/kvx/... -v

# Integration test: Verify consistent output
scafctl auth status -o json
scafctl auth status -e _ -o json
# Both should produce identical output
```

### Regression Verification

Ensure all resolver-based CEL expressions still work:
- `_.field` access (most common pattern)
- Nested access: `_.config.database.host`
- With `__self`, `__item`, `__index` variables
- Transform and validate expressions

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Resolver expressions break | Low | High | All resolvers pass `map[string]any`, so `_.field` access unchanged |
| CEL compilation errors | Low | Medium | `DynType` is more permissive than `MapType`, not less |
| Performance impact | Very Low | Low | No significant change in CEL evaluation path |

## Breaking Changes

This change is intentionally breaking:

1. **API Signature Change**: `BuildCELContext` and `EvaluateExpression` now accept `any` instead of `map[string]any` for the root data parameter.

2. **Output Behavior Change**: Commands using `-e _` will now return the raw data instead of a wrapped `{"value": ...}` object.

Both changes improve consistency and correctness.

## Implementation Status

✅ **COMPLETED** - Implemented on 2026-02-05

### Changes Made

1. **`pkg/celexp/context.go`**:
   - Changed `BuildCELContext` parameter from `resolverData map[string]any` to `rootData any`
   - Changed `EvaluateExpression` parameter from `resolverData map[string]any` to `rootData any`
   - Changed `_` variable declaration from `MapType(StringType, DynType)` to `DynType`
   - Updated all docstrings and examples

2. **`pkg/terminal/kvx/cel.go`**:
   - Removed wrapping logic in `EvaluateWithScafctlCEL`
   - Now passes root data directly to `celexp.EvaluateExpression`

3. **`pkg/celexp/context_test.go`**:
   - Added tests for slice root data
   - Added tests for string root data
   - Added tests for int root data
   - Added tests for bool root data
   - Added tests for slice operations (size, index access, filter)

4. **`pkg/provider/builtin/celprovider/cel_test.go`**:
   - Updated `TestCelProvider_Execute_NoResolverData` test
   - Changed error expectation from compile-time ("failed to compile expression") to runtime ("no such key: input")
   - This reflects the correct behavior with DynType: missing keys are caught at runtime, not compile time

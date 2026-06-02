---
title: "State: Missing Features Implementation Plan"
weight: 15
---

# State: Missing Features Implementation Plan

This document tracks features described in the [state design doc](state.md) that are not yet
implemented in the codebase.

---

## Overview

The core state system is fully implemented (package, types, manager, lifecycle,
CLI commands, lint rules, file provider backend, state provider, fingerprinting).
Both previously-planned features are now also implemented:

| # | Feature | Status |
|---|---------|--------|
| 1 | Render command state support | **Implemented** -- see `pkg/cmd/scafctl/render/solution.go` (`loadStateIntoContext`) |
| 2 | MCP `state_set` tool | **Implemented** -- see `pkg/mcp/tools_state.go` |

The `github` provider state backend is intentionally out of scope -- it lives in
a separate repository and is not part of this project.

The sections below are kept for historical context and describe the original
design that was followed during implementation.

---

## 1. Render Command State Support

### Problem

`scafctl render solution` does not load state before executing resolvers. If a
solution uses the `state` provider to read previously stored values, those
resolvers will always return `null`/fallback in render mode -- even when a valid
state file exists.

### Design

Follow the exact pattern from `run/solution.go`:

1. Before calling `solrender.ExecuteResolvers()`, create a `state.Manager`
2. Call `stateMgr.Load(ctx, params, cmdInfo)` with subcommand `"render solution"`
3. If state is enabled and loads successfully, inject into context with
   `state.WithState(ctx, loadResult.Data)`
4. **Do NOT call `stateMgr.Save()`** after execution -- render is read-only

### Files to Modify

| File | Change |
|------|--------|
| `pkg/cmd/scafctl/render/solution.go` | Add state loading before each `ExecuteResolvers()` call site |

### Implementation Steps

1. Import `pkg/state` in the render package
2. Before resolver execution, check `sol.State != nil`
3. Create `state.NewManager(sol.State, reg, settings.VersionInformation.BuildVersion)`
4. Call `stateMgr.Load(ctx, params, cmdInfo)` -- use subcommand `"render solution"`
5. If `!loadResult.Skipped`, update `ctx = loadResult.Ctx`
6. Pass updated context to `ExecuteResolvers()`
7. No save call -- render is read-only

### Testing

- Unit test: render a solution that uses the `state` provider with an existing
  state file. Verify the resolver reads the stored value.
- Unit test: render a solution with state enabled but no state file exists.
  Verify the state provider returns fallback.
- Unit test: confirm no state file is written/modified after render.

---

## 2. MCP `state_set` Tool

### Problem

The CLI has `scafctl state set --path <file> --key <key> --value <value>` but the
MCP server only exposes `state_list`, `state_get`, and `state_delete`. Users of
the MCP interface (e.g., VS Code Copilot) cannot set state values programmatically.

### Design

Add a `state_set` tool following the same pattern as `state_delete`. Mark it as
destructive in annotations (it mutates state data).

### Input Schema

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | Yes | State file path relative to `paths.StateDir()` |
| `key` | string | Yes | State entry key |
| `value` | string | Yes | Value to store (string -- same as CLI behavior) |
| `type` | string | No | Type annotation (default: `"string"`) |

### Files to Modify

| File | Change |
|------|--------|
| `pkg/mcp/tools_state.go` | Add `state_set` tool definition and handler |

### Implementation Steps

1. Add tool definition in `registerStateTools()` with `mcp.NewTool()`
2. Mark as destructive in annotations (matches `state_delete` pattern)
3. Handler: extract `path`, `key`, `value`, `type` from request
4. Call `state.LoadFromFile()` to load existing state
5. Check immutability of existing entry (same as CLI `set` command)
6. Create/update the `Entry` with value, type, timestamp
7. Call `state.SaveToFile()` to persist
8. Return success message

### Testing

- Unit test: set a new key, verify it persists
- Unit test: set over an immutable key, verify error
- Unit test: set with type coercion

---

## 3. Design Doc Accuracy Fix

### Problem

The implementation status table in `docs/design/state.md` claims the `github`
provider state operations are "Done" with location
`pkg/provider/builtin/githubprovider/github_state.go`. This provider lives in a
separate repository.

### Fix

Update the status table entry to reflect the actual status:

| Current | Updated |
|---------|---------|
| `github` provider state operations \| Done \| `pkg/provider/builtin/githubprovider/github_state.go` | `github` provider state operations \| External \| Separate repository |

### Files to Modify

| File | Change |
|------|--------|
| `docs/design/state.md` | Update implementation status table, package layout table, and "Future Backends" table to note GitHub provider is external |

---

## Execution Order

1. Render command state support (unblocks state usage in render workflows)
2. Design doc accuracy fix (low effort, high clarity value)
3. MCP `state_set` tool (low priority, convenience feature)

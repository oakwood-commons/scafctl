---
title: Shared Library Migration Plan
weight: 103
---

# Shared Library Migration Plan

This document defines the prioritized plan for extracting business logic from CLI command packages (`pkg/cmd/scafctl/...`) and MCP handler files (`pkg/mcp/tools_*.go`) into proper shared domain packages (`pkg/...`). This is a prerequisite for adding a future API server layer and ensures all entry points (CLI, MCP, API) delegate to the same shared code with minimal duplication.

## Table of Contents

- [Motivation](#motivation)
- [Architecture Goal](#architecture-goal)
- [Current State](#current-state)
- [Migration Phases](#migration-phases)
  - [Phase 0: Trivial Moves (No Refactoring)](#phase-0-trivial-moves-no-refactoring)
  - [Phase 1: MCP Inline Extractions](#phase-1-mcp-inline-extractions)
  - [Phase 2: CLI Moderate Extractions](#phase-2-cli-moderate-extractions)
  - [Phase 3: CLI Heavy Extractions](#phase-3-cli-heavy-extractions)
- [Migration Rules](#migration-rules)
- [Dependency Direction](#dependency-direction)
- [Testing Strategy](#testing-strategy)
- [Tracking](#tracking)

---

## Motivation

Today, three concerns prevent the codebase from cleanly supporting CLI + MCP + API entry points:

1. **Shared logic lives in CLI packages.** The MCP server already imports 5 CLI command packages (`pkg/cmd/scafctl/{explain,lint,run,get/provider}`). A future API server would inherit the same anti-pattern dependency direction.

2. **MCP handlers contain ~560 lines of inline business logic** (scaffold YAML generation, solution diffing, example discovery, config sanitization) that cannot be reused by CLI commands or an API.

3. **CLI handlers contain ~6,500 lines of inline business logic** (build pipeline, graph rendering, bundle operations, crypto, auth orchestration) that cannot be reused by MCP tools or an API.

The fix is mechanical: extract shared logic into domain packages under `pkg/`, then make CLI, MCP, and API entry points thin wrappers.

---

## Architecture Goal

Every entry point should follow the same pattern:

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ CLI Command  │  │ MCP Handler  │  │ API Handler  │
│ (cobra)      │  │ (mcp-go)     │  │ (http/grpc)  │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                 │
       │  Parse flags/   │  Parse MCP      │  Parse HTTP
       │  args into       │  params into    │  request into
       │  domain types    │  domain types   │  domain types
       │                 │                 │
       ▼                 ▼                 ▼
┌─────────────────────────────────────────────────────┐
│              Shared Domain Package                   │
│  pkg/<domain>/                                       │
│  - Accepts domain types (no CLI/MCP/HTTP types)      │
│  - Returns domain types                              │
│  - Pure business logic                               │
│  - Full test coverage                                │
└─────────────────────────────────────────────────────┘
```

**Entry point responsibilities (the minimum):**
1. Parse input from the transport layer (flags, MCP params, HTTP body) into domain types
2. Call the shared domain function
3. Format the result for the transport layer (table/JSON output, MCP response, HTTP response)

**Shared package rules:**
- No imports from `pkg/cmd/`, `pkg/mcp/`, or any future `pkg/api/`
- No imports of `cobra`, `mcp-go`, `net/http`, `terminal/writer`, `terminal/kvx`, `settings.Run`
- Accept and return plain Go types, domain structs, or interfaces defined in domain packages
- All exported functions must be callable from any entry point

---

## Current State

### What's Already Clean

These shared packages exist and are properly delegated to by both CLI and MCP:

| Shared Package | Used by CLI | Used by MCP |
|---|---|---|
| `pkg/celexp` | — | `evaluate_cel`, `list_cel_functions` |
| `pkg/gotmpl` | — | `evaluate_go_template` |
| `pkg/catalog` | `catalog *` | `catalog_list`, `list_solutions` |
| `pkg/solution/soltesting` | `test *` | `run_solution_tests` |
| `pkg/provider` (registry) | `get provider` | `list_providers` |
| `pkg/solution/prepare` | `run *` | `lint_solution`, `dry_run_solution`, `preview_*` |
| `pkg/resolver` | `render solution`, `run resolver` | `render_solution` |
| `pkg/action` | `run solution` | `preview_action`, `dry_run_solution` |
| `pkg/auth` | `auth *` | `auth_status` |
| `pkg/config` | `config *` | `get_config` |
| `pkg/schema` | `config schema`, `explain schema` | `get_solution_schema`, `explain_kind` |

### What Needs Migration

#### Functions Currently in CLI Packages, Imported by MCP

| Current Location | Exported Functions | CLI Deps in Signature? | MCP Imports It? |
|---|---|---|---|
| `pkg/cmd/scafctl/explain/results.go` | `LoadSolution()`, `BuildSolutionExplanation()`, `LookupProvider()` | **No** | Yes |
| `pkg/cmd/scafctl/lint/lint.go` | `Solution()`, `FilterBySeverity()` | **No** | Yes |
| `pkg/cmd/scafctl/lint/rules.go` | `KnownRules`, `ListRules()`, `GetRule()` | **No** | Yes |
| `pkg/cmd/scafctl/run/execute.go` | `ExecuteResolvers()`, `ValidateSolution()`, `ResolverExecutionConfigFromContext()` | **No** | Yes |
| `pkg/cmd/scafctl/get/provider/provider.go` | `BuildProviderDetail()`, `BuildSchemaOutput()`, `GenerateCLIExamples()`, `SchemaPlaceholder()`, `CapabilitiesToStrings()` | **No** | Yes |

Key insight: the exported *functions* are already CLI-free in their signatures. The migration is primarily about relocating them to the correct package — not rewriting them.

#### Inline Logic in MCP Handlers

| File | Function | Lines | Depends On |
|---|---|---|---|
| `tools_scaffold.go` | `buildScaffoldYAML()` | ~170 | `fmt`, `strings` only |
| `tools_diff.go` | diff logic in `handleDiffSolution()` | ~150 | `explain.LoadSolution()` |
| `tools_examples.go` | `findExamplesDir()`, `scanExamples()`, `descriptionFromPath()` | ~120 | `os`, `filepath`, `runtime` |
| `tools_config.go` | `sanitizeConfig()` + 6 struct types | ~120 | `pkg/config` |

#### Inline Logic in CLI Handlers

| File | Lines | Description |
|---|---|---|
| `render/solution.go` | ~750 | Graph rendering (ASCII/DOT/Mermaid/JSON), snapshot, test output |
| `build/solution.go` | ~650 | Build pipeline: discovery, composition, vendoring, OCI |
| `bundle/diff.go` | ~550 | Solution diff computation, file/vendored/plugin diffs |
| `run/common.go` | ~400 | Solution loading orchestration, output formatting |
| `bundle/extract.go` | ~300 | Bundle extraction with filtering |
| `auth/login.go` | ~280 | Multi-provider login flow routing |
| `secrets/export.go` + `import.go` | ~320 | AES-256-GCM + PBKDF2 crypto |
| `vendor/update.go` | ~250 | Dependency re-resolution, lock file management |
| `snapshot/save.go` | ~150 | Save pipeline: execution, redaction, snapshot capture |
| `get/resolver/refs.go` | ~150 | Template/CEL reference extraction |
| `config/paths.go` | ~200 | Per-platform path table generation |
| `cache/clear.go` | ~200 | Dir walking, size calc, cache clearing |
| `get/celfunction/celfunction.go` | ~200 | Function listing/detail formatting |

---

## Migration Phases

### Phase 0: Trivial Moves (No Refactoring)

**Effort:** Low — files are already CLI-free. Move files, update imports, add package aliases.  
**Impact:** High — fixes the dependency direction for the 5 packages MCP already imports.  
**Risk:** Low — function signatures don't change. Only import paths change.

#### Phase 0A: `pkg/cmd/scafctl/explain/results.go` → `pkg/solution/inspect/`

**Why first:** Most imported shared file (used by MCP `tools_solution.go`, `tools_diff.go`, `tools_provider.go`, `resources.go`).

| Action | Detail |
|---|---|
| Create | `pkg/solution/inspect/inspect.go` |
| Move | `LoadSolution()`, `BuildSolutionExplanation()`, `LookupProvider()` + all 6 exported types |
| Update imports | `pkg/mcp/tools_solution.go`, `tools_diff.go`, `tools_provider.go`, `resources.go`, `pkg/cmd/scafctl/explain/*.go` |
| Old package | `pkg/cmd/scafctl/explain/results.go` becomes a thin re-export or is deleted (CLI `explain` commands update to import `pkg/solution/inspect/`) |
| Tests | Move `results_test.go` if it exists, or create tests in new location |

**New package API:**

```go
package inspect

import "github.com/oakwood-commons/scafctl/pkg/solution"

type SolutionExplanation struct { ... }  // existing struct
type ResolverInfo struct { ... }
type ActionInfo struct { ... }
// ... other types

func LoadSolution(ctx context.Context, path string) (*solution.Solution, error)
func BuildSolutionExplanation(sol *solution.Solution) *SolutionExplanation
func LookupProvider(ctx context.Context, name string, reg *provider.Registry) (*provider.Descriptor, error)
```

#### Phase 0B: `pkg/cmd/scafctl/run/execute.go` → `pkg/solution/execute/`

**Why:** Second most critical — `ExecuteResolvers()` is the core resolver execution engine used by MCP `tools_solution.go` and `tools_dryrun.go`.

| Action | Detail |
|---|---|
| Create | `pkg/solution/execute/execute.go` |
| Move | `ValidateSolution()`, `ExecuteResolvers()`, `ResolverExecutionConfigFromContext()` + 3 exported types |
| Update imports | `pkg/mcp/tools_solution.go`, `tools_dryrun.go`, `pkg/cmd/scafctl/run/*.go` |

**New package API:**

```go
package execute

type SolutionValidationResult struct { ... }
type ResolverExecutionConfig struct { ... }
type ResolverExecutionResult struct { ... }

func ValidateSolution(ctx context.Context, sol *solution.Solution, reg *provider.Registry) *SolutionValidationResult
func ExecuteResolvers(ctx context.Context, sol *solution.Solution, params map[string]any, reg *provider.Registry, cfg ResolverExecutionConfig) (*ResolverExecutionResult, error)
func ResolverExecutionConfigFromContext(ctx context.Context) ResolverExecutionConfig
```

#### Phase 0C: `pkg/cmd/scafctl/lint/{lint.go,rules.go}` → `pkg/lint/`

**Why:** Lint engine and rule registry are used by MCP `tools_lint.go` and `tools_solution.go`.

| Action | Detail |
|---|---|
| Create | `pkg/lint/lint.go` — the `Solution()` function, `FilterBySeverity()`, all unexported lint helpers (`lintResolvers`, `lintWorkflow`, `lintAction`, etc.), types (`SeverityLevel`, `Finding`, `Result`) |
| Create | `pkg/lint/rules.go` — `KnownRules`, `RuleMeta`, `ListRules()`, `GetRule()` |
| Keep in CLI | `pkg/cmd/scafctl/lint/cmd.go` — only `CommandLint()`, `Options`, `runLint()` (thin wrapper calling `lint.Solution()`) |
| Update imports | `pkg/mcp/tools_lint.go`, `tools_solution.go` |

**Note:** The `lint.go` file in the CLI package has ~900 lines. The exported functions (`Solution()`, `FilterBySeverity()`) and all the unexported domain functions (`lintResolvers`, `lintWorkflow`, `lintAction`, `lintExpressions`, `validateCELSyntax`, `validateTemplateSyntax`, `collectReferencedResolvers`, `lintResultSchema`, etc.) should all move to `pkg/lint/`. What remains in the CLI package is just the cobra command constructor and output formatting — roughly 100 lines.

#### Phase 0D: `pkg/cmd/scafctl/get/provider/` shared functions → `pkg/provider/detail/`

**Why:** `BuildProviderDetail()` and `BuildSchemaOutput()` are used by MCP `tools_provider.go` and `resources.go`.

| Action | Detail |
|---|---|
| Create | `pkg/provider/detail/detail.go` |
| Move | `BuildProviderDetail()`, `BuildSchemaOutput()`, `GenerateCLIExamples()`, `SchemaPlaceholder()`, `CapabilitiesToStrings()` |
| Keep in CLI | `pkg/cmd/scafctl/get/provider/` — `CommandProvider()`, `Options`, TUI code (`tui.go`), CLI-specific formatting |
| Update imports | `pkg/mcp/tools_provider.go`, `resources.go` |

#### Phase 0 Completion Criteria

After Phase 0, the dependency graph becomes:

```
pkg/mcp         → pkg/solution/inspect, pkg/solution/execute, pkg/lint, pkg/provider/detail
pkg/cmd/scafctl → pkg/solution/inspect, pkg/solution/execute, pkg/lint, pkg/provider/detail
pkg/api (future)→ pkg/solution/inspect, pkg/solution/execute, pkg/lint, pkg/provider/detail
```

No entry point package imports another entry point package. This is the critical fix.

---

### Phase 1: MCP Inline Extractions

**Effort:** Low-Medium — functions are already standalone, just need to be moved and exported.  
**Impact:** Medium — enables new CLI commands (`scafctl new`, `scafctl examples`) from the MCP enhancements plan.  
**Risk:** Low — no signature changes to existing consumers.

#### Phase 1A: `pkg/mcp/tools_scaffold.go` → `pkg/scaffold/`

| Action | Detail |
|---|---|
| Create | `pkg/scaffold/scaffold.go` |
| Move | `buildScaffoldYAML()` → export as `Solution()` |
| Define | `Options` struct (Name, Description, Version, Features, Providers) |
| Define | `Result` struct (YAML string, Filename, Features list) |
| Update | `pkg/mcp/tools_scaffold.go` — call `scaffold.Solution()` |
| Enables | `scafctl new solution` CLI command (Phase 1B of mcp-server-enhancements.md) |

**New package API:**

```go
package scaffold

type Options struct {
    Name        string            `json:"name" yaml:"name"`
    Description string            `json:"description" yaml:"description"`
    Version     string            `json:"version" yaml:"version"`
    Features    map[string]bool   `json:"features" yaml:"features"`
    Providers   []string          `json:"providers" yaml:"providers"`
}

type Result struct {
    YAML     string   `json:"yaml" yaml:"yaml"`
    Filename string   `json:"filename" yaml:"filename"`
    Features []string `json:"features" yaml:"features"`
}

func Solution(opts Options) (*Result, error)
```

#### Phase 1B: `pkg/mcp/tools_diff.go` → `pkg/soldiff/`

| Action | Detail |
|---|---|
| Create | `pkg/soldiff/diff.go` |
| Extract | Inline diff logic from `handleDiffSolution()` into `Compare()` |
| Define | `Change` struct (Field, Type, Old, New, OldValue, NewValue) |
| Define | `DiffResult` struct (Changes, Summary by type, HasDifferences bool) |
| Update | `pkg/mcp/tools_diff.go` — call `soldiff.Compare()` |
| Enables | `scafctl diff solution` CLI command, API endpoint |

**New package API:**

```go
package soldiff

type Change struct {
    Field    string `json:"field" yaml:"field"`
    Type     string `json:"type" yaml:"type"`       // "added", "removed", "changed"
    Old      string `json:"old,omitempty" yaml:"old,omitempty"`
    New      string `json:"new,omitempty" yaml:"new,omitempty"`
    OldValue any    `json:"oldValue,omitempty" yaml:"oldValue,omitempty"`
    NewValue any    `json:"newValue,omitempty" yaml:"newValue,omitempty"`
}

type DiffResult struct {
    Changes        []Change       `json:"changes" yaml:"changes"`
    Summary        map[string]int `json:"summary" yaml:"summary"`
    HasDifferences bool           `json:"hasDifferences" yaml:"hasDifferences"`
}

func Compare(a, b *solution.Solution) *DiffResult
```

#### Phase 1C: `pkg/mcp/tools_examples.go` → `pkg/examples/`

| Action | Detail |
|---|---|
| Create | `pkg/examples/examples.go` |
| Move | `findExamplesDir()`, `scanExamples()`, `descriptionFromPath()`, `exampleItem` type |
| Export | As `FindDir()`, `Scan()`, `DescriptionFromPath()`, `Item` |
| Consider | Replace `runtime.Caller`/`findExamplesDir` with `go:embed` (as recommended in mcp-server-enhancements.md) |
| Update | `pkg/mcp/tools_examples.go` — call `examples.Scan()`, `examples.FindDir()` |
| Enables | `scafctl examples list/get` CLI commands |

#### Phase 1D: `pkg/mcp/tools_config.go` → `pkg/config/sanitize.go`

| Action | Detail |
|---|---|
| Create | `pkg/config/sanitize.go` (inside existing `pkg/config/` package) |
| Move | `sanitizeConfig()` → export as `Sanitize()`, all sanitized struct types |
| Update | `pkg/mcp/tools_config.go` — call `config.Sanitize()` |
| Enables | API `GET /config` endpoint returning redacted config |

This one goes into the existing `pkg/config/` package since it operates directly on `config.Config`.

---

### Phase 2: CLI Moderate Extractions

**Effort:** Medium — requires untangling domain logic from CLI-specific output formatting.  
**Impact:** Medium — enables MCP tools for operations currently CLI-only.  
**Risk:** Medium — must ensure CLI behavior doesn't regress.

#### Phase 2A: `pkg/cmd/scafctl/secrets/{export,import}.go` crypto → `pkg/secrets/crypto/`

| Action | Detail |
|---|---|
| Create | `pkg/secrets/crypto/crypto.go` |
| Move | `encryptExport()` → `Encrypt()`, `decryptExport()` → `Decrypt()` |
| Move | Constants: `pbkdf2Iterations`, `pbkdf2KeySize`, `pbkdf2SaltSize` |
| Move | Types: `ExportFormat`, `ExportedSecret` |
| Keep in CLI | `CommandExport()`, `CommandImport()` (flag parsing, password prompting, file I/O) |

#### Phase 2B: `pkg/cmd/scafctl/snapshot/save.go` → `pkg/resolver/snapshot/`

| Action | Detail |
|---|---|
| Create | `pkg/resolver/snapshot/save.go` |
| Extract | Save pipeline: provider setup → resolver execution → redaction → snapshot capture |
| Define | `SaveOptions` struct, `Save(ctx, opts) (*resolver.Snapshot, error)` |
| Keep in CLI | `CommandSave()` (flag parsing, file output) |
| Enables | MCP `save_snapshot` tool, API endpoint |

#### Phase 2C: `pkg/cmd/scafctl/get/resolver/refs.go` → `pkg/resolver/refs/`

| Action | Detail |
|---|---|
| Create | `pkg/resolver/refs/refs.go` |
| Extract | Template/CEL reference extraction logic |
| Define | `ExtractRefs(sol *solution.Solution) ([]Ref, error)` |
| Keep in CLI | Command constructor, output formatting |
| Enables | MCP `extract_resolver_refs` tool (Phase 2A of mcp-server-enhancements.md) |

#### Phase 2D: `pkg/cmd/scafctl/config/paths.go` logic → `pkg/paths/`

| Action | Detail |
|---|---|
| Extract | Per-platform path table generation into `pkg/paths/` (which already exists) |
| Define | `AllPaths() []PathInfo` that returns structured path data |
| Keep in CLI | Command constructor, table formatting |
| Enables | MCP `get_config_paths` tool (Phase 2H of mcp-server-enhancements.md) |

#### Phase 2E: `pkg/cmd/scafctl/get/celfunction/` → `pkg/celexp/detail/`

| Action | Detail |
|---|---|
| Create | `pkg/celexp/detail/detail.go` |
| Extract | Function listing, detail building, formatting |
| Keep in CLI | Command constructor, TUI, output formatting |

#### Phase 2F: `pkg/cmd/scafctl/cache/` logic → `pkg/cache/`

| Action | Detail |
|---|---|
| Create | `pkg/cache/cache.go` |
| Extract | `Info() CacheInfo`, `Clear(opts ClearOptions) error` |
| Keep in CLI | Command constructor, confirmation prompts, output formatting |

#### Phase 2G: `pkg/cmd/scafctl/run/common.go` shared logic → `pkg/solution/execute/`

| Action | Detail |
|---|---|
| Move | `filterResolversWithDependencies()`, `calculateValueSize()` into `pkg/solution/execute/` (created in Phase 0B) |
| Extract | `prepareSolutionForExecution()` core logic (without CLI types) into a shared function |
| Keep in CLI | `sharedResolverOptions`, `addSharedResolverFlags`, output writing, progress bars |
| Note | This package is where `run/execute.go` was moved in Phase 0B. This phase extends it. |

---

### Phase 3: CLI Heavy Extractions

**Effort:** High — large files with deeply interleaved CLI and domain logic.  
**Impact:** High — unlocks the largest blocks of functionality for MCP/API.  
**Risk:** Higher — extensive refactoring, more test surface area.

#### Phase 3A: `pkg/cmd/scafctl/lint/lint.go` engine → `pkg/lint/`

This extends Phase 0C. Phase 0C moves the exported functions and types. Phase 3A moves the **unexported lint rule implementations** (~700 lines):

| Functions to Move | Lines |
|---|---|
| `lintResolvers()` | ~55 |
| `lintWorkflow()` | ~65 |
| `lintAction()` | ~50 |
| `lintResultSchema()` | ~40 |
| `lintExpressions()` | ~30 |
| `validateCELSyntax()` | ~15 |
| `validateTemplateSyntax()` | ~5 |
| `collectReferencedResolvers()` | ~50 |
| `scanInputsForResolverRefs()` | ~50 |
| `lintProviderInputs()` | ~80 |
| `registryAdapter` type | ~15 |
| Additional helpers | ~remaining |

**Note:** Phase 0C and Phase 3A can be combined into a single phase if preferred — the split is only to provide an early checkpoint.

#### Phase 3B: `pkg/cmd/scafctl/build/solution.go` → `pkg/solution/builder/`

Note: `pkg/solution/bundler/` already exists. The build orchestration is distinct from bundling.

| Action | Detail |
|---|---|
| Create | `pkg/solution/builder/builder.go` |
| Extract | Build pipeline: solution discovery → validation → composition → vendoring → dedup → hash → bundle creation |
| Define | `BuildOptions`, `BuildResult`, `Build(ctx, opts) (*BuildResult, error)` |
| Keep in CLI | `CommandBuildSolution()` (flag parsing, progress output, OCI push orchestration) |
| Complexity | High — interleaves file I/O, catalog lookups, and bundler calls |

#### Phase 3C: `pkg/cmd/scafctl/render/solution.go` → `pkg/solution/render/`

| Action | Detail |
|---|---|
| Create | `pkg/solution/render/render.go` |
| Extract | `RenderResolverGraph()`, `RenderActionGraph()` for multiple formats (ASCII, DOT, Mermaid, JSON) |
| Extract | Snapshot creation logic, test output generation |
| Define | `RenderOptions`, `RenderResult` |
| Keep in CLI | `CommandSolution()` (flag parsing, output formatting) |
| Note | Some rendering already exists in `pkg/resolver` (`RenderASCII`, `RenderMermaid`) and `pkg/action` (`BuildVisualization`). This phase consolidates the orchestration layer. |

#### Phase 3D: `pkg/cmd/scafctl/bundle/{diff,extract,verify}.go` → `pkg/solution/bundler/`

| Action | Detail |
|---|---|
| Extend | `pkg/solution/bundler/` (already exists with core bundling logic) |
| Move | Diff computation logic → `bundler.Diff()` |
| Move | Extraction logic → `bundler.Extract()` (may already partially exist) |
| Move | Verification logic → `bundler.Verify()` |
| Keep in CLI | Command constructors, output formatting |

#### Phase 3E: `pkg/cmd/scafctl/auth/login.go` → `pkg/auth/`

| Action | Detail |
|---|---|
| Extend | `pkg/auth/` (already exists) |
| Extract | Flow routing, handler construction with config overrides |
| Define | `LoginOptions`, `Login(ctx, opts) error` |
| Keep in CLI | `CommandLogin()`, interactive prompts, browser launch |
| Complexity | Medium-High — multiple auth providers, interactive flows |

#### Phase 3F: `pkg/cmd/scafctl/vendor/update.go` → `pkg/solution/bundler/vendor/`

| Action | Detail |
|---|---|
| Create | `pkg/solution/bundler/vendor/vendor.go` |
| Extract | Dependency re-resolution, vendored file update, lock file management |
| Define | `UpdateOptions`, `Update(ctx, opts) (*UpdateResult, error)` |
| Keep in CLI | Command constructor, output formatting |

---

## Migration Rules

These rules apply to every phase:

### 1. One Phase at a Time
Complete each phase fully (including tests, import updates, and CI passing) before starting the next. Do not partially extract.

### 2. No Behavior Changes
Extractions must be pure mechanical moves. Do not change logic, add features, or fix bugs during extraction. File issues for anything discovered.

### 3. Re-Export Pattern for Backward Compatibility
When moving functions out of a CLI package, consider temporarily re-exporting from the old location to avoid a big-bang import update:

```go
// pkg/cmd/scafctl/explain/results.go (after move)
package explain

import "github.com/oakwood-commons/scafctl/pkg/solution/inspect"

// Deprecated: Use pkg/solution/inspect.LoadSolution directly.
var LoadSolution = inspect.LoadSolution
```

Remove re-exports in a follow-up PR once all callers are updated. Since we allow breaking changes, this is optional — direct import path updates in a single PR are also acceptable.

### 4. Test Migration
- Move tests alongside the code they test
- Verify test count doesn't decrease
- Run full test suite after each phase

### 5. Import Update Verification
After each phase, verify no `pkg/cmd/scafctl/` imports remain in `pkg/mcp/` for the migrated functionality:

```bash
grep -r 'pkg/cmd/scafctl/' pkg/mcp/ --include='*.go' | grep -v '_test.go'
```

The goal is to reduce this list to zero.

---

## Dependency Direction

### Before Migration

```
pkg/mcp ──→ pkg/cmd/scafctl/explain    ← WRONG (MCP depends on CLI)
pkg/mcp ──→ pkg/cmd/scafctl/lint       ← WRONG
pkg/mcp ──→ pkg/cmd/scafctl/run        ← WRONG
pkg/mcp ──→ pkg/cmd/scafctl/get/provider ← WRONG
pkg/mcp ──→ pkg/celexp, pkg/gotmpl, pkg/catalog, ...  ← CORRECT
```

### After Migration (Phase 0 complete)

```
pkg/mcp ──→ pkg/solution/inspect       ← CORRECT (MCP depends on domain)
pkg/mcp ──→ pkg/lint                   ← CORRECT
pkg/mcp ──→ pkg/solution/execute       ← CORRECT
pkg/mcp ──→ pkg/provider/detail        ← CORRECT
pkg/mcp ──→ pkg/celexp, pkg/gotmpl, pkg/catalog, ...  ← CORRECT (unchanged)

pkg/cmd/scafctl/explain ──→ pkg/solution/inspect  ← CORRECT (CLI depends on domain)
pkg/cmd/scafctl/lint    ──→ pkg/lint              ← CORRECT
pkg/cmd/scafctl/run     ──→ pkg/solution/execute  ← CORRECT
```

### After Full Migration

```
pkg/mcp ──────────┐
pkg/cmd/scafctl ──┼──→ pkg/{domain packages only}
pkg/api (future) ─┘

No cross-dependencies between entry point packages.
```

---

## Testing Strategy

### Per-Phase Testing

| Phase | Test Approach |
|---|---|
| Phase 0 | Existing tests move with code. Run `go test ./...` — zero failures expected. Run MCP integration tests. |
| Phase 1 | New unit tests for exported functions in new packages (90% coverage target). MCP handler tests should still pass unchanged. |
| Phase 2 | New unit tests for extracted functions. CLI command tests should still pass unchanged. |
| Phase 3 | New unit tests + update CLI tests to test through shared package. Most involved testing phase. |

### Validation Command

After each phase:

```bash
# Verify no new CLI imports in MCP
grep -rn 'pkg/cmd/scafctl/' pkg/mcp/ --include='*.go' | grep -v _test.go

# Run all tests
go test ./... -count=1

# Run linter
golangci-lint run

# Run integration tests
go test ./tests/integration/... -count=1
```

---

## Tracking

### Phase 0: Trivial Moves

| ID | Task | New Package | Status |
|---|---|---|---|
| 0A | Move `explain/results.go` exports | `pkg/solution/inspect/` | Not Started |
| 0B | Move `run/execute.go` exports | `pkg/solution/execute/` | Not Started |
| 0C | Move `lint/` exports (types + `Solution()` + `FilterBySeverity()` + `KnownRules` + `ListRules()` + `GetRule()`) | `pkg/lint/` | Not Started |
| 0D | Move `get/provider/` shared functions | `pkg/provider/detail/` | Not Started |

### Phase 1: MCP Inline Extractions

| ID | Task | New Package | Status |
|---|---|---|---|
| 1A | Extract scaffold YAML generation | `pkg/scaffold/` | Not Started |
| 1B | Extract solution diff logic | `pkg/soldiff/` | Not Started |
| 1C | Extract examples discovery | `pkg/examples/` | Not Started |
| 1D | Extract config sanitization | `pkg/config/` (extend) | Not Started |

### Phase 2: CLI Moderate Extractions

| ID | Task | New Package | Status |
|---|---|---|---|
| 2A | Extract secrets crypto | `pkg/secrets/crypto/` | Not Started |
| 2B | Extract snapshot save pipeline | `pkg/resolver/snapshot/` | Not Started |
| 2C | Extract resolver refs extraction | `pkg/resolver/refs/` | Not Started |
| 2D | Extract config paths logic | `pkg/paths/` (extend) | Not Started |
| 2E | Extract CEL function detail | `pkg/celexp/detail/` | Not Started |
| 2F | Extract cache operations | `pkg/cache/` | Not Started |
| 2G | Extract run/common.go shared logic | `pkg/solution/execute/` (extend) | Not Started |

### Phase 3: CLI Heavy Extractions

| ID | Task | New Package | Status |
|---|---|---|---|
| 3A | Move remaining lint rule implementations | `pkg/lint/` (extend) | Not Started |
| 3B | Extract build pipeline | `pkg/solution/builder/` | Not Started |
| 3C | Extract render orchestration | `pkg/solution/render/` | Not Started |
| 3D | Extract bundle diff/extract/verify | `pkg/solution/bundler/` (extend) | Not Started |
| 3E | Extract auth login orchestration | `pkg/auth/` (extend) | Not Started |
| 3F | Extract vendor update logic | `pkg/solution/bundler/vendor/` | Not Started |

---

## Summary

| Phase | Tasks | Effort | Impact | Lines Moved |
|---|---|---|---|---|
| **Phase 0** | 4 | Low | **Critical** — fixes dependency direction | ~1,200 |
| **Phase 1** | 4 | Low-Medium | Medium — enables new CLI/API commands | ~560 |
| **Phase 2** | 7 | Medium | Medium — unlocks moderate CLI logic | ~1,400 |
| **Phase 3** | 6 | High | High — unlocks largest code blocks | ~3,300 |
| **Total** | 21 | — | — | ~6,500 |

**Recommended start:** Phase 0 — it has the highest impact-to-effort ratio and is prerequisite for everything else.

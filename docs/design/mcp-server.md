---
title: MCP Server for scafctl Solutions
weight: 100
---

# MCP Server for scafctl Solutions

## Overview

This document evaluates building a [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server for scafctl solutions. An MCP server exposes **tools**, **resources**, and **prompts** over JSON-RPC 2.0 (typically via stdio or SSE) so that AI agents (Claude, Copilot, etc.) can discover and invoke them programmatically.

## How an MCP Server Works Internally

### The Big Picture

An MCP server is a **long-running process** that sits between an AI agent (e.g., Claude, Copilot) and your application. It speaks a specific protocol (JSON-RPC 2.0) so the AI knows what capabilities are available and how to call them.

Think of it like a REST API, but purpose-built for AI agents instead of web browsers.

### Lifecycle

1. **Startup**: The AI client launches the MCP server process (e.g., `scafctl mcp serve`). This starts a persistent process that listens for JSON-RPC messages over **stdio** (stdin/stdout pipes) or **SSE** (HTTP server-sent events).

2. **Capability Discovery**: The AI client sends an `initialize` request. The server responds with a manifest of everything it can do — its list of tools (with input schemas), resources (with URI templates), and prompts. This is how the AI learns *"I can call `lint_solution` with a `file` parameter"*.

3. **Tool Invocation**: When the AI decides to use a tool, it sends a JSON-RPC `tools/call` request with the tool name and arguments. The server executes the operation and returns the result as a JSON-RPC response.

4. **Shutdown**: When the AI client disconnects, the server process exits.

### What Happens When an AI Calls a Tool

Here is the flow when an AI agent calls, for example, `lint_solution`:

```
AI Agent                    MCP Server (scafctl)              scafctl Libraries
   │                              │                                  │
   │  {"method": "tools/call",    │                                  │
   │   "params": {                │                                  │
   │     "name": "lint_solution", │                                  │
   │     "arguments": {           │                                  │
   │       "file": "solution.yaml"│                                  │
   │     }                        │                                  │
   │   }}                         │                                  │
   │ ──────────────────────────►  │                                  │
   │                              │  solution.LoadFromBytes(bytes)   │
   │                              │ ──────────────────────────────►  │
   │                              │                                  │
   │                              │  solution.Validate()             │
   │                              │ ──────────────────────────────►  │
   │                              │                                  │
   │                              │  ◄── []LintFinding              │
   │                              │                                  │
   │  {"result": {                │                                  │
   │    "content": [{             │                                  │
   │      "type": "text",         │                                  │
   │      "text": "2 warnings..." │                                  │
   │    }]                        │                                  │
   │  }}                          │                                  │
   │ ◄────────────────────────── │                                  │
```

### It Is NOT Shelling Out to the CLI

The MCP server would **not** run `scafctl lint solution.yaml` as a subprocess. Instead, it imports and calls the same Go library functions that the CLI commands use internally. This is the key architectural difference:

| Approach | How It Works | Pros | Cons |
|----------|-------------|------|------|
| **Library calls** (recommended) | MCP handler calls `solution.LoadFromBytes()`, `resolver.Execute()`, etc. directly as Go function calls | Fast, type-safe, structured output, proper error handling | Requires the MCP server to be part of the scafctl binary |
| **Subprocess/CLI wrapping** | MCP handler runs `scafctl lint ...` as a child process and parses stdout | Simpler to implement, decoupled | Slow (process spawn per call), fragile text parsing, loses structured data |

Since scafctl already has clean library packages (`pkg/solution/`, `pkg/resolver/`, `pkg/provider/`, etc.) that are separate from the Cobra CLI layer, the library-call approach is straightforward. The MCP tool handlers would be very similar to what the CLI commands already do — just returning JSON instead of writing to a terminal.

### Concrete Example: What a Tool Handler Looks Like

A simplified example of what the `lint_solution` MCP tool handler would look like in Go:

```go
func handleLintSolution(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
    filePath := args["file"].(string)

    // Read the solution file (same as CLI does)
    data, err := os.ReadFile(filePath)
    if err != nil {
        return mcp.NewToolResultError("file not found: " + err.Error()), nil
    }

    // Use the existing library to load + validate (same code path as `scafctl lint`)
    sol := &solution.Solution{}
    if err := sol.LoadFromBytes(data); err != nil {
        return mcp.NewToolResultError("invalid solution: " + err.Error()), nil
    }

    // Run linting (same code path as `scafctl lint`)
    findings := linter.Lint(ctx, sol)

    // Return structured result to the AI
    result, _ := json.Marshal(findings)
    return mcp.NewToolResultText(string(result)), nil
}
```

This is essentially the same logic as the CLI command, minus the Cobra flag parsing and terminal formatting.

### What the AI Agent Sees

From the AI's perspective, it sees a list of tools with schemas, like:

```json
{
  "name": "lint_solution",
  "description": "Validate a scafctl solution file and return lint findings",
  "inputSchema": {
    "type": "object",
    "properties": {
      "file": {
        "type": "string",
        "description": "Path to the solution YAML file"
      }
    },
    "required": ["file"]
  }
}
```

The AI decides *when* to call tools based on the user's request. For example, if a user says "check if my solution is valid", the AI would call `lint_solution` and then interpret the results in natural language.

### Where the Server Runs

The MCP server runs **locally on the user's machine**, in the same security context as the user. It has the same filesystem access, network access, and credentials as the user running it. This is important for:

- **Auth**: The server inherits the user's auth tokens (Entra, GitHub, GCP) from the scafctl config
- **Filesystem**: It can read solution files from the user's projects
- **Catalog**: It can access local and remote catalogs using the user's configuration

The AI client (VS Code, Claude Desktop, etc.) connects to this local server process — it does not expose anything to the network (when using stdio transport).

### Working Directory Override

All MCP tools that accept file paths support an optional `cwd` parameter. This allows AI agents to specify the project directory for path resolution without requiring the MCP server process to be started from that directory. See the [cwd design doc](cwd.md) for details.

## What Is Required

### 1. New Go Dependency

There is no MCP library in `go.mod` today. We would add one:

- **[`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go)** — the most popular Go MCP SDK, supports stdio + SSE transports
- Alternatively, hand-roll the JSON-RPC 2.0 protocol (not recommended — ~500 LoC of boilerplate)

### 2. New Package + Command

- **`pkg/mcp/`** — Server implementation, tool/resource registration
- **`pkg/cmd/scafctl/mcp/`** — New `scafctl mcp serve` CLI command (starts the MCP server on stdio or SSE)

### 3. MCP Tools to Expose

| Tool | Maps To | Description |
|------|---------|-------------|
| `list_solutions` | `scafctl get solution` | List available solutions from local catalog |
| `inspect_solution` | `scafctl get solution <name>` | Get solution metadata, resolvers, actions |
| `run_solution` | `scafctl run solution` | Execute a solution with parameters |
| `run_resolver` | `scafctl run resolver` | Run resolvers only (no actions) |
| `lint_solution` | `scafctl lint` | Validate a solution file |
| `render_solution` | `scafctl render solution` | Render without executing |
| `list_providers` | `scafctl get provider` | List available providers |
| `run_provider` | `scafctl run provider` | Execute a single provider |
| `catalog_list` | `scafctl catalog list` | List catalog entries |
| `catalog_inspect` | `scafctl catalog inspect` | Inspect artifact metadata (includes multi-platform info) |
| `catalog_list_platforms` | — | List platforms for a multi-platform plugin artifact |
| `build_plugin` | `scafctl build plugin` | Build multi-platform plugin into local catalog |
| `catalog_pull` | `scafctl catalog pull` | Pull a solution from registry |
| `test_solution` | `scafctl test functional` | Run functional tests |
| `explain_solution` | `scafctl explain` | Explain a configuration |

### 4. MCP Resources to Expose

| Resource | Description |
|----------|-------------|
| `solution://{name}` | Solution YAML content |
| `solution://{name}/schema` | JSON Schema for solution inputs |
| `solution://{name}/graph` | Dependency graph (resolver + action) |
| `provider://{name}` | Provider detail including input/output schemas, examples, CLI usage |
| `provider://reference` | Compact reference of all providers with required/optional inputs |
| `catalog://` | Catalog index |

### 5. Wiring / Integration Points

The existing code is well-factored for this. The key integration points:

- **`pkg/solution/get.Getter`** already handles loading from catalog/file/URL/auto-discovery
- **`pkg/resolver/executor.go`** has a clean `Execute()` entry point
- **`pkg/action/executor.go`** is similarly encapsulated
- **`pkg/provider/registry.go`** gives us provider discovery
- **`pkg/solution/solution.go`** `LoadFromBytes()` handles parse + validate

We would essentially be creating thin adapters from MCP tool handlers → existing library functions.

## How Hard Would It Be

**Estimated effort: Medium — roughly 1-2 weeks of focused work**

> **Note:** The preparatory refactoring (extracting command logic, creating the MCP context helper, adding the `cel-functions` command, etc.) has already been completed. The remaining effort is the MCP server implementation itself.

| Component | Effort | Rationale |
|-----------|--------|-----------|
| MCP server scaffold + stdio transport | ~1 day | Straightforward with `mcp-go` SDK |
| Core tools (list, inspect, lint, render) | ~2-3 days | Thin wrappers around existing packages — read-only, low risk |
| `run_solution` / `run_resolver` tools | ~2-3 days | Needs careful handling of: input parameter passing, streaming output, timeout/cancellation, error reporting |
| MCP resources (solution content, schemas) | ~1 day | Straightforward reads from catalog/filesystem |
| Testing | ~2-3 days | Unit tests for each tool handler + integration test for the MCP server end-to-end |
| Docs + tutorials + examples | ~1 day | Per project conventions |

### What Makes It Easier Than Expected

- The codebase has **clean separation** between CLI commands and library logic — we are not fighting Cobra coupling
- **Preparatory refactoring is complete** — `prepare.Solution()`, `ValidateSolution()`, `ExecuteResolvers()`, `BuildSolutionExplanation()`, `LookupProvider()`, and `mcp.NewContext()` are all ready to be called from MCP tool handlers
- Providers, resolvers, and solutions all have well-defined interfaces
- `context.Context` is used throughout, so cancellation propagates naturally
- The KVX output system already supports JSON, making tool responses trivial
- The `scafctl get cel-functions` command proves the `ext.All()` API works end-to-end for the `list_cel_functions` MCP tool

### What Makes It Harder

- **Streaming/progress**: Solutions can be long-running. MCP tools are request/response, not streaming. We would need to either: (a) block until complete and return the full result, (b) implement MCP's progress notifications, or (c) return a "job ID" resource for polling
- **Interactive resolvers**: The `parameter` provider prompts for user input — in MCP context, we would need to either require all params upfront or use MCP's `sampling` capability to ask the AI to provide them
- **Auth context**: Solutions may need auth tokens (Entra, GitHub, GCP). The MCP server would need to either inherit the CLI's auth config or accept credentials as tool inputs
- **Side effects**: `run_solution` modifies the world (creates files, calls APIs). AI agents calling this need guardrails — we would want a confirmation/dry-run pattern

## What Would Be the Benefit

**High value — this is where the industry is heading.** Concrete benefits:

### 1. AI-Assisted Solution Authoring

An AI agent could inspect existing solutions, understand provider schemas, and help write new solutions — with real validation feedback by calling `lint_solution` in a loop.

### 2. Natural Language Solution Execution

Instead of learning CLI flags, users say *"Deploy the GCP infrastructure solution with project=my-project and region=us-east1"* — the AI translates to a `run_solution` tool call with the right parameters.

### 3. IDE Integration

VS Code (via Copilot), Cursor, Windsurf, and other AI-enabled editors support MCP. Users editing solution YAML could get real-time validation, auto-completion suggestions, and execution without leaving the editor.

### 4. Catalog Discovery

AI can browse the catalog, compare solutions, and recommend the right one — much more accessible than `scafctl catalog list | grep ...`.

### 5. Debugging Workflows

When a solution fails, an AI agent could automatically inspect the resolver graph, re-run individual resolvers, check provider outputs, and diagnose the issue.

### 6. Composability

Other MCP-aware tools could chain scafctl operations. For example: a CI/CD agent could pull a solution, run its tests, and deploy — all through MCP tool calls.

### 7. Reduced Onboarding Friction

New users don't need to learn the CLI — they describe what they want, and the AI figures out which scafctl commands to run.

## Codebase Readiness Assessment

An analysis of the codebase revealed that ~70% of the operations an MCP server needs were already callable as clean Go library functions. The preparatory refactoring described below has been **completed** — the codebase is now ready for MCP server implementation.

### Already MCP-Ready (No Changes Needed)

These core packages can be called directly from an MCP handler today:

| Package | API | Notes |
|---|---|---|
| `pkg/celexp/` | `EvaluateExpression(ctx, expr, data, vars)` | Fully self-contained, thread-safe |
| `pkg/provider/registry.go` | `Get()`, `List()`, `ListByCapability()` | Thread-safe, no CLI dependencies |
| `pkg/solution/get/` | `NewGetter().Get(ctx, path)` | Clean dependency injection, no CLI deps |
| `pkg/resolver/` | `NewExecutor(registry).Execute(ctx, resolvers, params)` | Clean library API |
| `pkg/action/` | `BuildGraph()`, `NewExecutor().Execute()` | Clean library API |
| `pkg/config/` | `NewManager("").Load()` | Reads from filesystem and env vars only |
| `pkg/schema/` | `IntrospectType()`, `GetKind()`, `GenerateConfigSchema()` | Returns structured data |
| `pkg/settings/` | `NewCliParams()` | Stateless, supports `FromAPI: true` for non-CLI usage |
| Lint logic | `lintSolution(sol, path, registry)` | Already a pure function in the lint command |

### Completed Preparatory Refactoring

The following changes were implemented to make MCP integration cleaner and improve the codebase independently (better testability, cleaner separation of concerns). All items are **complete** — tests pass and linter is clean.

#### 1. Extract Command Logic from Cobra `RunE` Closures ✅

**Problem:** Command logic lived inside `SolutionOptions.Run(ctx)` methods on Options structs that bundled CLI concerns (`IOStreams`, `KvxOutputFlags`, `CliParams`) with execution config (`ResolverTimeout`, `PhaseTimeout`). An MCP handler could not call these without constructing fake CLI scaffolding.

**Implementation:** Created `pkg/cmd/scafctl/run/execute.go` with standalone functions callable from both CLI and MCP:

```go
// pkg/cmd/scafctl/run/execute.go
func ValidateSolution(ctx context.Context, sol *solution.Solution, reg *provider.Registry) *SolutionValidationResult
func ExecuteResolvers(ctx context.Context, sol *solution.Solution, params map[string]any, reg *provider.Registry, cfg ResolverExecutionConfig) (*ResolverExecutionResult, error)
func ResolverExecutionConfigFromContext(ctx context.Context) ResolverExecutionConfig
```

Structured result types (`SolutionValidationResult`, `ResolverExecutionResult`, `ResolverExecutionConfig`) provide type-safe outputs for MCP tool handlers. 5 unit tests in `execute_test.go`.

**Files:** `pkg/cmd/scafctl/run/execute.go`, `pkg/cmd/scafctl/run/execute_test.go`

#### 2. Extract `prepareSolutionForExecution` as a Standalone Function ✅

**Problem:** This helper bundled solution loading + registry setup + bundle extraction + provider registration, but it was a method on the CLI-specific `SolutionOptions` struct.

**Implementation:** Created `pkg/solution/prepare/prepare.go` with a standalone function using the functional options pattern:

```go
// pkg/solution/prepare/prepare.go
func Solution(ctx context.Context, path string, opts ...Option) (*Result, error)

// Functional options
func WithGetter(g get.Interface) Option
func WithRegistry(r *provider.Registry) Option
func WithStdin(reader io.Reader) Option
func WithMetrics(errOut io.Writer) Option
```

Returns `Result{Solution, Registry, Cleanup}`. The CLI's `prepareSolutionForExecution` now delegates to this function. Removed 3 dead methods (`getOrCreateGetter`, `loadSolutionWithBundle`, `getRegistry`) and their unused imports from `common.go`. 9 unit tests in `prepare_test.go`.

**Files:** `pkg/solution/prepare/prepare.go`, `pkg/solution/prepare/prepare_test.go`, `pkg/cmd/scafctl/run/common.go` (refactored)

#### 3. Make `explain` Commands Return Structured Data ✅

**Problem:** The `explain` commands (`solution`, `provider`, `schema`) wrote formatted text imperatively via `Writer` rather than building a struct first. An MCP handler needs structured data, not terminal text.

**Implementation:** Created `pkg/cmd/scafctl/explain/results.go` with structured types and exported helper functions:

```go
// pkg/cmd/scafctl/explain/results.go
type SolutionExplanation struct { ... }  // Full solution metadata
type CatalogInfo struct { ... }          // Catalog metadata
type ResolverInfo struct { ... }         // Resolver details
type ActionInfo struct { ... }           // Action details

func LoadSolution(ctx context.Context, path string) (*solution.Solution, error)
func BuildSolutionExplanation(sol *solution.Solution) *SolutionExplanation
func LookupProvider(ctx context.Context, name string, reg *provider.Registry) (*provider.Descriptor, error)
```

All types have JSON/YAML tags. The CLI `explain solution` and `explain provider` commands now call these functions and format the results. MCP tools can call the same functions and return the structs as JSON.

**Files:** `pkg/cmd/scafctl/explain/results.go`, `pkg/cmd/scafctl/explain/solution.go` (refactored), `pkg/cmd/scafctl/explain/provider.go` (refactored), `pkg/cmd/scafctl/explain/solution_test.go` (updated), `pkg/cmd/scafctl/explain/provider_test.go` (updated)

#### 4. Fix Stray `os.Exit` Call ✅

**Problem:** `pkg/cmd/scafctl/secrets/exists.go` called `os.Exit(1)` directly instead of returning an error. This would crash an MCP server process.

**Implementation:** Replaced with `exitcode.WithCode(fmt.Errorf("secret %q does not exist", name), exitcode.GeneralError)` and removed the unused `"os"` import.

**Files:** `pkg/cmd/scafctl/secrets/exists.go`

#### 5. Create an MCP Context Helper ✅

**Problem:** Several libraries silently pull values from `context.Context` (logger, config, auth registry, writer). An MCP handler needs to know exactly which values to inject.

**Implementation:** Created `pkg/mcp/context.go` with a functional options pattern:

```go
// pkg/mcp/context.go
func NewContext(opts ...ContextOption) context.Context

func WithConfig(cfg *config.Config) ContextOption
func WithLogger(lgr logr.Logger) ContextOption
func WithAuthRegistry(reg *auth.Registry) ContextOption
func WithSettings(params *settings.CliParams) ContextOption
func WithIOStreams(streams *terminal.IOStreams) ContextOption
```

Provides sensible defaults: discard logger, empty auth registry, quiet/no-color settings, discard IO streams + no-op writer. 7 unit tests in `context_test.go`.

**Files:** `pkg/mcp/context.go`, `pkg/mcp/context_test.go`

#### 6. Add `scafctl get cel-functions` CLI Command ✅

**Problem:** scafctl extends CEL with ~25 custom functions (`map.merge`, `json.unmarshal`, `filepath.join`, `guid.new`, `time.now`, etc.) plus standard CEL extensions (~50+ functions). The data was available programmatically via `ext.All()` in `pkg/celexp/ext/ext.go`, but there was no CLI command for users to discover available CEL functions.

**Implementation:** Created `pkg/cmd/scafctl/get/celfunction/celfunction.go` with the `scafctl get cel-functions` command:

- **Aliases:** `cel-funcs`, `cel`, `cf`
- **Modes:** List all functions (table view) or get detail on a specific function
- **Filters:** `--custom` (scafctl-only) and `--built-in` (standard CEL) flags
- **Output:** Full KVX integration (`-o json/yaml/table/quiet`)
- **Testable:** Function injection pattern for unit testing without CEL dependencies

12 unit tests in `celfunction_test.go`, 7 integration tests added to `tests/integration/cli_test.go`.

**Files:** `pkg/cmd/scafctl/get/celfunction/celfunction.go`, `pkg/cmd/scafctl/get/celfunction/celfunction_test.go`, `pkg/cmd/scafctl/get/get.go` (wired up), `tests/integration/cli_test.go` (integration tests)

#### 7. Initialize CEL Factories at Server Startup ✅

**Problem:** `celexp.SetEnvFactory()` and `SetCacheFactory()` use `sync.Once` — they can only be called once per process. The MCP server needs to call these during initialization.

**Resolution:** No code change needed. The factory setters are already called during the CLI's `PersistentPreRun` initialization. Since the MCP server runs as a subcommand (`scafctl mcp serve`), it inherits this initialization. The MCP server startup path will call these factory setters as part of its initialization sequence.

### Preparatory Refactoring Summary

| Change | Status | Key Files |
|---|---|---|
| Extract `ValidateSolution` / `ExecuteResolvers` from Options | ✅ Complete | `pkg/cmd/scafctl/run/execute.go` |
| Extract `prepare.Solution` standalone function | ✅ Complete | `pkg/solution/prepare/prepare.go` |
| Make `explain` commands return structs | ✅ Complete | `pkg/cmd/scafctl/explain/results.go` |
| Fix `secrets/exists.go` `os.Exit` | ✅ Complete | `pkg/cmd/scafctl/secrets/exists.go` |
| Create `mcp.NewContext` helper | ✅ Complete | `pkg/mcp/context.go` |
| Add `scafctl get cel-functions` CLI command | ✅ Complete | `pkg/cmd/scafctl/get/celfunction/celfunction.go` |
| CEL factory initialization at startup | ✅ No change needed | Inherited from `PersistentPreRun` |

**All preparatory refactoring is complete.** The codebase is ready for MCP server implementation.

### What Does NOT Need to Change

- **Provider registry, CEL evaluation, solution getter, resolver/action executors, config, settings, schema introspection** — all ready as-is
- **The `RootOptions` pattern** with injectable `IOStreams` and `ExitFunc` already supports in-process execution (integration tests use this today)
- **JSON/YAML serialization** works everywhere since all structs have proper tags
- **The `Writer` system** — MCP handlers bypass it entirely and work with structured return values. For library code that pulls Writer from context, a no-op or buffer-backed Writer works fine (`terminal.NewTestIOStreams()` already exists for this)

## Recommended Approach

An incremental rollout was used. All read-only phases are now **complete**.

1. ~~**Preparatory refactoring** (~3-4 days)~~ — **✅ Complete.** Extracted command logic from Cobra closures, fixed `os.Exit`, created MCP context helper, added `cel-functions` command. See [Completed Preparatory Refactoring](#completed-preparatory-refactoring) for details.
2. ~~**Start with read-only tools** (`list_solutions`, `inspect_solution`, `lint_solution`, `list_providers`)~~ — **✅ Complete.** All Phase 2 tools implemented in `pkg/mcp/tools_*.go`.
3. ~~**Add `render_solution`** and evaluation/catalog tools~~ — **✅ Complete.** Phase 3 tools (`evaluate_cel`, `render_solution`, `auth_status`, `catalog_list`) implemented.
4. ~~**MCP Resources**~~ — **✅ Complete.** `solution://{name}` and `solution://{name}/schema` resource templates implemented. `provider://{name}` and `provider://reference` resources added for comprehensive provider schema access.
5. ~~**Schema, Examples & Prompts**~~ — **✅ Complete.** Added `get_solution_schema` and `explain_kind` tools (Huma-based JSON Schema generation), `list_examples` and `get_example` tools, and 4 MCP prompts (`create_solution`, `debug_solution`, `add_resolver`, `add_action`).
6. ~~**Testing**~~ — **✅ Complete.** Unit tests for all tool handlers, resources, server, and context. Integration tests for CLI commands and MCP protocol.
7. ~~**Documentation, Tutorials & Examples**~~ — **✅ Complete.** Tutorial at `docs/tutorials/mcp-server-tutorial.md`, example configs at `examples/mcp/`.
8. ~~**Solution Developer Experience Enhancements**~~ — **✅ Complete.** Added 6 new tools (`evaluate_go_template`, `validate_expression`, `explain_lint_rule`, `scaffold_solution`, `preview_action`, `diff_solution`), 1 new resource (`solution://{name}/graph`), 1 new prompt (`compose_solution`), plus `resolver` filter on `preview_resolvers` and `verbose` mode on `run_solution_tests`.
9. **Add `run_solution` with `--dry-run` default** — preview execution without side effects (deferred to future release)
10. **Add full `run_solution`** behind explicit confirmation — the AI must surface the plan before executing (deferred to future release)

This incremental approach lets us ship value at each step while managing the risk of AI-triggered side effects.

### Implementation Details

For the detailed implementation guide covering all phases, tool schemas, file maps, and test coverage, see [MCP Server Implementation Guide](./mcp-server-implementation-guide.md).

For the user-facing tutorial, see [MCP Server Tutorial](../tutorials/mcp-server-tutorial.md).

Example configurations for AI clients are in `examples/mcp/`.

## Decisions

### Subcommand vs Separate Binary

**Decision: Subcommand (`scafctl mcp serve`)**

The MCP server will be a subcommand of the existing `scafctl` binary, not a separate binary. This is the industry-standard pattern used by Terraform (`terraform mcp serve`), GitHub CLI (`gh mcp serve`), kubectl, Docker, and others.

Rationale:

- **Single binary distribution** — Users already have `scafctl` installed. No separate install step, no version synchronization between two binaries.
- **Shared initialization** — The MCP server needs the same config loading, auth setup, CEL factory initialization, provider registry, and plugin discovery that the CLI already does in `PersistentPreRun`. As a subcommand, it gets all of this for free.
- **Library call approach requires it** — Since we are calling Go library functions directly (not shelling out), the MCP server must be compiled into the same binary to access `pkg/solution/`, `pkg/provider/`, etc.
- **Versioning is automatic** — The MCP server version always matches the CLI version. No compatibility matrix.
- **Standard MCP client configuration** — AI clients expect a single command:
  ```json
  {
    "mcpServers": {
      "scafctl": {
        "command": "scafctl",
        "args": ["mcp", "serve"]
      }
    }
  }
  ```

A separate binary would only make sense if the MCP server had significantly different dependencies that would bloat the CLI (not the case — `mcp-go` is small), needed to run as a shared remote service (not our use case), or was owned by a different team.

### Transport: stdio First, SSE Later

**Decision: Ship with stdio as the default transport. Add SSE support later behind a `--transport` flag.**

The primary use case for the MCP server is through VS Code. stdio is VS Code's preferred and default transport for local MCP servers.

#### How stdio Works in VS Code

VS Code reads MCP server definitions from `.vscode/mcp.json` or workspace settings. A stdio configuration looks like:

```json
// .vscode/mcp.json
{
  "servers": {
    "scafctl": {
      "type": "stdio",
      "command": "scafctl",
      "args": ["mcp", "serve"]
    }
  }
}
```

Or equivalently in workspace settings:

```json
// .vscode/settings.json
{
  "mcp": {
    "servers": {
      "scafctl": {
        "type": "stdio",
        "command": "scafctl",
        "args": ["mcp", "serve"]
      }
    }
  }
}
```

When Copilot (or any MCP-aware extension) activates, VS Code:
1. Spawns `scafctl mcp serve` as a child process
2. Sends `initialize` over stdin, reads response from stdout
3. Discovers tools and their schemas
4. Sends `tools/call` over stdin when the AI decides to invoke a tool, reads result from stdout
5. On window close or config change, sends shutdown and kills the process

This is the same pattern VS Code uses for language servers (LSP), which also default to stdio transport.

#### Why stdio First

- **Zero configuration** — No port to pick, no port conflicts, no "address already in use" errors, no macOS firewall popups. It just works.
- **Security by default** — stdio is inherently local. No network socket to accidentally expose. SSE requires binding to a port, which could theoretically be accessed by other processes.
- **One server per client** — Each VS Code window spawns its own `scafctl mcp serve` process. Clean lifecycle — client disconnects, process exits. No orphan servers.
- **Industry standard** — Terraform, GitHub CLI, Docker, kubectl all use stdio for their local MCP servers. Every example in the VS Code MCP documentation uses stdio for local tools.
- **Simpler implementation** — With `mcp-go`, stdio transport is ~5 lines of setup versus SSE which needs HTTP server configuration, CORS, port selection, graceful shutdown, and health checks.

#### SSE for the Future

SSE will be added later to support:
- **Remote/shared servers** — A team running one MCP server that multiple developers connect to (e.g., a shared catalog browser)
- **Web-based AI clients** — Browser-based tools that cannot spawn local processes
- **Long-lived servers** — MCP servers that need to stay running independently of any single AI client session

The `--transport` flag ensures SSE can be added without a breaking change:

```
scafctl mcp serve                              # stdio (default)
scafctl mcp serve --transport sse --port 8080  # future
```

The transport layer is independent of tool handler code in `mcp-go`, so no tool implementations need to change when SSE is added.

### Authentication Passthrough

**Decision: Inherit the user's existing auth context. No special auth mechanism needed.**

Since the MCP server runs as a local subprocess (`scafctl mcp serve`), it inherits the same security context as any other scafctl command — same environment variables, same filesystem, same keychain access. Authentication works identically to the CLI with no additional configuration.

#### How It Works

1. The user authenticates before starting the MCP server by running `scafctl auth login <provider>` (same as they would before running solutions from the CLI).
2. The MCP server's `PersistentPreRun` initializes the auth registry from cached tokens, environment variables, and system credential stores — the same code path as every other scafctl command.
3. VS Code spawns the process in the user's shell environment, so all env vars (`GITHUB_TOKEN`, `GOOGLE_APPLICATION_CREDENTIALS`, Azure env vars, etc.) are present.
4. Solutions that need cloud credentials access them through the auth registry in context, exactly as they do today.

**In other words: if `scafctl run solution` can authenticate today, `scafctl mcp serve` will authenticate the same way with zero additional work.**

#### Edge Cases

**Token expiry during long sessions:** The CLI is short-lived, but the MCP server may run for hours. If an OAuth token expires mid-session, the auth handlers should refresh tokens automatically (most already do). If they cannot, the tool call returns a clear error and the AI instructs the user to run `scafctl auth login` again.

**Interactive auth flows:** The MCP server communicates over stdio and cannot open a browser for OAuth. Auth must be set up before starting the server. This is the same model as other CLI-based MCP servers.

**Per-solution credential requirements:** Different solutions may need different cloud credentials. The MCP server handles this the same way the CLI does — solutions fail at runtime with a clear error if a required token is missing. The AI can then relay this to the user.

#### Enhancements

- **`auth_status` tool** — Expose a tool that reports which auth providers are configured and whether tokens are valid. The AI can check auth status proactively before attempting execution and tell the user exactly what to set up.
- **`inspect_solution` auth metadata** — If solutions declare which auth providers they require in metadata, the inspect tool could surface this so the AI can verify auth before running.

### Read-Only Tools Initially

**Decision: Limit to read-only tools for the initial release. Execution tools (`run_solution`, `run_resolver`, `run_provider`) will be added later.**

The primary use case is AI-assisted solution authoring — helping users create solutions, understand schemas, generate CEL expressions, and validate configurations. All of this is read-only. Execution tools introduce complexity (side effects, guardrails, confirmation flows, streaming) that is not needed for the initial value proposition.

#### Initial Tool Set

| Tool | Purpose | Read-Only? |
|------|---------|------------|
| `list_solutions` | List available solutions from catalog | Yes |
| `inspect_solution` | Get solution metadata, resolvers, actions | Yes |
| `lint_solution` | Validate a solution file | Yes |
| `list_providers` | List available providers and their schemas | Yes |
| `get_provider_schema` | Get JSON Schema for a provider's inputs | Yes |
| `render_solution` | Render action graph without executing (includes `crossSectionRefs` for finally→main references) | Yes |
| `list_cel_functions` | List all available CEL functions (built-in + scafctl custom) | Yes |
| `list_go_template_functions` | List all available Go template extension functions (Sprig + custom) | Yes |
| `evaluate_cel` | Test a CEL expression against sample data | Yes |
| `explain_solution` | Explain a solution's configuration | Yes |
| `explain_provider` | Explain a provider's capabilities and inputs | Yes |
| `auth_status` | Report which auth providers are configured | Yes |
| `catalog_list` | List catalog entries | Yes |

#### Deferred to Future Release

| Tool | Reason to Defer |
|------|-----------------|
| `run_solution` | Side effects, needs confirmation/dry-run patterns |
| `run_resolver` | May trigger external calls depending on providers |
| `run_provider` | Side effects for action-capable providers |
| `catalog_pull` | Modifies local catalog state |
| `test_solution` | Executes resolvers and actions in test mode |

#### Why Read-Only First

- **Lower risk** — No accidental file creation, API calls, or infrastructure changes triggered by AI
- **No guardrail complexity** — No need to implement confirmation prompts, dry-run defaults, or rate limiting yet
- **Faster to ship** — Read-only tools are thin wrappers around existing library functions with no streaming or progress concerns
- **Covers the primary use case** — Solution authoring, schema help, CEL generation, and validation are all read-only operations
- **Resolves the "destructive operations" open question** — By deferring execution tools, rate limiting and confirmation prompts are also deferred

### Preparatory Refactoring as a Standalone PR

**Decision: Yes, the preparatory refactoring was done as a standalone PR before MCP work begins.**

The refactoring (extracting command logic from Cobra closures, fixing the stray `os.Exit`, creating the MCP context helper, adding the `cel-functions` command, etc.) was submitted as its own PR. This keeps the MCP implementation PR focused on new functionality rather than mixing refactoring with feature work, and the refactoring improvements (better testability, cleaner separation of concerns) benefit the codebase regardless of MCP.

See [Completed Preparatory Refactoring](#completed-preparatory-refactoring) for the full list of changes and file references.

## Advanced Protocol Features

The MCP server leverages several advanced features from the mcp-go SDK (v0.44.0):

### Observability Hooks & Middleware

All MCP requests are instrumented with timing and logging via `server.Hooks`:
- `BeforeAny` / `OnSuccess` / `OnError` hooks log request lifecycle
- `BeforeCallTool` / `AfterCallTool` hooks track tool execution duration
- `OnRegisterSession` / `OnUnregisterSession` track client connections
- Separate tool and resource timing middleware layers

Implementation: `pkg/mcp/hooks.go`

### Structured Errors

All tool error responses use a consistent structured format (`ToolError`) with:
- Machine-readable error code (`INVALID_INPUT`, `NOT_FOUND`, `LOAD_FAILED`, etc.)
- Contextual field name identifying which input caused the error
- Actionable suggestions for resolution
- Related tool names that may help

Implementation: `pkg/mcp/errors.go`

### Auto-Completion

The server provides completion suggestions for prompt arguments and resource template URIs:
- Provider names from the registry
- Migration types, solution features
- Solution names from the local catalog
- Lint rule names, CEL function names, example names

Implementation: `pkg/mcp/completions.go`

### Contextual Tool Filtering

A `ToolFilterFunc` dynamically hides tools whose required capabilities are unavailable:
- Auth tools hidden when no auth handlers configured
- Catalog tools hidden when no catalogs configured
- Provider tools hidden when no registry available

Implementation: `pkg/mcp/filter.go`

### Transport Protocols

The server supports three transports via CLI flags:
- **stdio** (default): JSON-RPC 2.0 over stdin/stdout
- **sse**: Server-Sent Events over HTTP (for remote/multi-client)
- **http**: Streamable HTTP transport

Implementation: `pkg/mcp/server.go` (`Serve`, `ServeSSE`, `ServeHTTP`)

### Additional SDK Capabilities

- **Structured Log Streaming**: `SendLog()` sends log messages to clients respecting their log level
- **Workspace Roots**: `RequestRoots()` discovers client workspace directories
- **Sampling**: `RequestSampling()` requests LLM completions from the client
- **Elicitation**: `RequestElicitation()` requests structured user input
- **Resource Notifications**: `NotifyResourcesChanged()` and `NotifyToolsChanged()` for change propagation
- **Pagination**: Configurable via `WithPaginationLimit()`
- **Tool & Resource Capabilities**: `listChanged` enabled for dynamic registration

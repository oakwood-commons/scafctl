---
title: MCP Server Implementation Guide
weight: 101
---

# MCP Server Implementation Guide

This document is the detailed, step-by-step implementation guide for building the MCP server into scafctl. It is the companion to the [MCP Server design document](./mcp-server.md) which covers the _why_ — this document covers the _how_.

## Prerequisites

Before starting implementation, the following must be true:

- All preparatory refactoring is **complete** (see [mcp-server.md § Completed Preparatory Refactoring](./mcp-server.md#completed-preparatory-refactoring))
- `pkg/mcp/context.go` exists with `NewContext()` and functional options
- `pkg/cmd/scafctl/run/execute.go` exports `ValidateSolution()`, `ExecuteResolvers()`, `ResolverExecutionConfigFromContext()`
- `pkg/solution/prepare/prepare.go` exports `Solution()` with functional options
- `pkg/cmd/scafctl/explain/results.go` exports `BuildSolutionExplanation()`, `LoadSolution()`, `LookupProvider()`
- `pkg/cmd/scafctl/get/celfunction/celfunction.go` provides the `cel-functions` command

## Decisions Summary

These decisions were made during planning and are **final** for this implementation:

| Decision | Choice | Rationale |
|----------|--------|-----------|
| `inspect_solution` vs `explain_solution` | **Merged** into single `inspect_solution` | Both return `SolutionExplanation` — one tool, one schema, AI extracts what it needs |
| Tool naming convention | **`snake_case`** | MCP specification convention; consistent with `get_weather`, `list_files`, etc. used in the MCP spec examples |
| Package layout for tools | **Per-domain files** | `tools_solution.go`, `tools_provider.go`, `tools_cel.go`, `tools_catalog.go`, `tools_auth.go` — scales as tools are added |
| `evaluate_cel` file support | **Both raw string and file-based context** | `celexp.EvaluateExpression()` already exists and accepts `rootData any` + `additionalVars map[string]any`; supporting files is a thin wrapper using `os.ReadFile` + YAML unmarshal — both modes for free |
| Progress notifications | **Implement from the start** | `mcp-go` v0.44.0 supports `ProgressNotification` natively; all tools that do I/O or computation should send progress |
| `--info` flag | **Yes** | `scafctl mcp serve --info` prints server capabilities and tool list as JSON, then exits — useful for debugging |
| SDK | **`mark3labs/mcp-go` v0.44.0+** | 8.2k stars, 170 contributors, used by 2.8k projects, implements MCP spec 2025-11-25 with backward compat |

---

## Phase 1: Scaffold & Infrastructure ✅ COMPLETE

**Estimated effort: ~1 day** | **Actual: completed**

This phase creates the MCP server skeleton, the CLI command, and the dependency wiring. No tools are implemented yet — just the empty server that responds to `initialize` and `tools/list` (returning an empty list).

### Step 1.1: Add `mcp-go` Dependency

```bash
go get github.com/mark3labs/mcp-go@latest
```

This adds:
- `github.com/mark3labs/mcp-go/mcp` — Types, tool builders, result helpers
- `github.com/mark3labs/mcp-go/server` — Server implementation, stdio/SSE transports

### Step 1.2: Create `pkg/mcp/server.go` — Server Core

**File:** `pkg/mcp/server.go`

This file defines the `Server` struct and its construction. The server owns the `mcp-go` MCPServer instance and holds references to shared dependencies (provider registry, auth registry, config).

```go
package mcp

import (
    "context"
    "encoding/json"
    "fmt"
    "os"

    "github.com/go-logr/logr"
    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    "github.com/oakwood-commons/scafctl/pkg/auth"
    "github.com/oakwood-commons/scafctl/pkg/config"
    "github.com/oakwood-commons/scafctl/pkg/provider"
)

// Server wraps the mcp-go MCPServer and holds shared dependencies
// that tool handlers need.
type Server struct {
    mcpServer *server.MCPServer
    ctx       context.Context
    logger    logr.Logger
    registry  *provider.Registry
    authReg   *auth.Registry
    config    *config.Config
    version   string
}

// ServerOption configures the MCP server.
type ServerOption func(*serverConfig)

type serverConfig struct {
    logger   *logr.Logger
    registry *provider.Registry
    authReg  *auth.Registry
    config   *config.Config
    version  string
    ctx      context.Context
}

// WithServerLogger sets the logger for the MCP server.
func WithServerLogger(lgr logr.Logger) ServerOption {
    return func(c *serverConfig) {
        c.logger = &lgr
    }
}

// WithServerRegistry sets the provider registry.
func WithServerRegistry(reg *provider.Registry) ServerOption {
    return func(c *serverConfig) {
        c.registry = reg
    }
}

// WithServerAuthRegistry sets the auth registry.
func WithServerAuthRegistry(reg *auth.Registry) ServerOption {
    return func(c *serverConfig) {
        c.authReg = reg
    }
}

// WithServerConfig sets the application config.
func WithServerConfig(cfg *config.Config) ServerOption {
    return func(c *serverConfig) {
        c.config = cfg
    }
}

// WithServerVersion sets the server version string.
func WithServerVersion(version string) ServerOption {
    return func(c *serverConfig) {
        c.version = version
    }
}

// WithServerContext sets the base context for the server.
func WithServerContext(ctx context.Context) ServerOption {
    return func(c *serverConfig) {
        c.ctx = ctx
    }
}
```

**Key design points:**

- The `Server` struct holds a **pre-built `context.Context`** (created via `mcp.NewContext()`) that all tool handlers receive. This context has the logger, config, auth registry, writer, and settings already injected.
- Tool handlers access shared state through the `Server` receiver — not through globals.
- The `mcpServer` field is the `mcp-go` `*server.MCPServer` which handles JSON-RPC, tool dispatch, and transport.

#### Server Construction

```go
// NewServer creates a new MCP server with all tools and resources registered.
func NewServer(opts ...ServerOption) (*Server, error) {
    cfg := &serverConfig{
        version: "dev",
    }
    for _, opt := range opts {
        opt(cfg)
    }

    // Build the MCP context for tool handlers
    var ctxOpts []ContextOption
    if cfg.config != nil {
        ctxOpts = append(ctxOpts, WithConfig(cfg.config))
    }
    if cfg.logger != nil {
        ctxOpts = append(ctxOpts, WithLogger(*cfg.logger))
    }
    if cfg.authReg != nil {
        ctxOpts = append(ctxOpts, WithAuthRegistry(cfg.authReg))
    }
    mcpCtx := NewContext(ctxOpts...)

    // If a parent context was provided, layer its cancellation
    if cfg.ctx != nil {
        mcpCtx = mergeContext(cfg.ctx, mcpCtx)
    }

    s := &Server{
        ctx:      mcpCtx,
        version:  cfg.version,
        registry: cfg.registry,
        authReg:  cfg.authReg,
        config:   cfg.config,
    }
    if cfg.logger != nil {
        s.logger = *cfg.logger
    } else {
        s.logger = logr.Discard()
    }

    // Create the mcp-go server
    s.mcpServer = server.NewMCPServer(
        "scafctl",
        cfg.version,
        server.WithToolCapabilities(false),    // No listChanged for now
        server.WithResourceCapabilities(true, false), // Subscribe=true, listChanged=false
        server.WithRecovery(),                 // Recover from panics in handlers
        server.WithInstructions(serverInstructions),
    )

    // Register all tools
    s.registerTools()

    // Register all resources
    s.registerResources()

    return s, nil
}

const serverInstructions = `scafctl is a CLI tool for managing infrastructure solutions using CEL expressions, 
Go templates, and a provider-based architecture. This MCP server exposes read-only tools 
for inspecting solutions, validating configurations, evaluating CEL expressions, and 
browsing the solution catalog. All tools are safe to call — they do not modify files, 
create resources, or trigger side effects.`
```

#### Serve Methods

```go
// Serve starts the MCP server on stdio transport (blocking).
func (s *Server) Serve() error {
    return server.ServeStdio(s.mcpServer)
}

// Info returns the server's tool and resource information as JSON.
// Used by `scafctl mcp serve --info`.
func (s *Server) Info() ([]byte, error) {
    type toolInfo struct {
        Name        string `json:"name"`
        Description string `json:"description"`
    }
    type serverInfo struct {
        Name    string     `json:"name"`
        Version string     `json:"version"`
        Tools   []toolInfo `json:"tools"`
    }

    info := serverInfo{
        Name:    "scafctl",
        Version: s.version,
    }

    // Extract tool info from registered tools
    for _, t := range s.tools() {
        info.Tools = append(info.Tools, toolInfo{
            Name:        t.Name,
            Description: t.Description,
        })
    }

    return json.MarshalIndent(info, "", "  ")
}
```

#### Tool Registration Pattern

```go
// registerTools registers all MCP tools on the server.
func (s *Server) registerTools() {
    // Solution tools
    s.registerSolutionTools()

    // Provider tools
    s.registerProviderTools()

    // CEL tools
    s.registerCELTools()

    // Catalog tools
    s.registerCatalogTools()

    // Auth tools
    s.registerAuthTools()
}
```

Each `register*Tools()` method lives in its own file (`tools_solution.go`, etc.) and calls `s.mcpServer.AddTool(tool, handler)` for each tool in that domain.

### Step 1.3: Create `pkg/cmd/scafctl/mcp/` — CLI Commands

**File:** `pkg/cmd/scafctl/mcp/mcp.go`

Parent command group following the existing pattern from `get.go`:

```go
package mcp

import (
    "github.com/spf13/cobra"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
)

// CommandMCP creates the `scafctl mcp` parent command.
func CommandMCP(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
    cmd := &cobra.Command{
        Use:          "mcp",
        Short:        "MCP (Model Context Protocol) server for AI agent integration",
        Long:         `Manage the MCP server that exposes scafctl capabilities to AI agents like GitHub Copilot, Claude, and Cursor.`,
        SilenceUsage: true,
    }
    cmd.AddCommand(CommandServe(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cmd.Use)))
    return cmd
}
```

**File:** `pkg/cmd/scafctl/mcp/serve.go`

The `scafctl mcp serve` command:

```go
package mcp

import (
    "encoding/json"
    "fmt"
    "os"

    "github.com/spf13/cobra"
    "github.com/oakwood-commons/scafctl/pkg/auth"
    "github.com/oakwood-commons/scafctl/pkg/config"
    "github.com/oakwood-commons/scafctl/pkg/logger"
    mcpserver "github.com/oakwood-commons/scafctl/pkg/mcp"
    "github.com/oakwood-commons/scafctl/pkg/provider"
    "github.com/oakwood-commons/scafctl/pkg/provider/builtin"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
)

// ServeOptions holds the options for the serve command.
type ServeOptions struct {
    Transport string
    LogFile   string
    Info      bool
    CliParams *settings.Run
    IOStreams  *terminal.IOStreams
}

// CommandServe creates the `scafctl mcp serve` command.
func CommandServe(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
    opts := &ServeOptions{
        CliParams: cliParams,
        IOStreams:  ioStreams,
    }

    cmd := &cobra.Command{
        Use:   "serve",
        Short: "Start the MCP server",
        Long: `Start the Model Context Protocol (MCP) server for AI agent integration.

The MCP server exposes scafctl capabilities as tools that AI agents can discover 
and invoke programmatically. It communicates over stdio (JSON-RPC 2.0) by default.

Example VS Code configuration (.vscode/mcp.json):

  {
    "servers": {
      "scafctl": {
        "type": "stdio",
        "command": "scafctl",
        "args": ["mcp", "serve"]
      }
    }
  }

Use --info to print the server's capabilities and exit (useful for debugging):

  scafctl mcp serve --info`,
        SilenceUsage: true,
        RunE: func(cmd *cobra.Command, args []string) error {
            return runServe(cmd.Context(), opts)
        },
    }

    cmd.Flags().StringVar(&opts.Transport, "transport", "stdio", "Transport protocol (stdio)")
    cmd.Flags().StringVar(&opts.LogFile, "log-file", "", "Write server logs to file (default: stderr)")
    cmd.Flags().BoolVar(&opts.Info, "info", false, "Print server capabilities as JSON and exit")

    return cmd
}
```

The `runServe` function:

```go
func runServe(ctx context.Context, opts *ServeOptions) error {
    // Get dependencies from context (injected by PersistentPreRun)
    lgr := logger.FromContext(ctx)
    cfg, _ := config.FromContext(ctx) // May be nil if no config file
    authReg := auth.RegistryFromContext(ctx)

    // Configure logging: MCP stdio uses stdout, so logs must go elsewhere
    var serverLogger logr.Logger
    if opts.LogFile != "" {
        // TODO: Create file-based logger
        serverLogger = lgr
    } else {
        // Log to stderr (safe — MCP stdio only uses stdout)
        serverLogger = lgr
    }

    // Build provider registry
    reg := builtin.DefaultRegistry(ctx)

    // Build server options
    serverOpts := []mcpserver.ServerOption{
        mcpserver.WithServerLogger(serverLogger),
        mcpserver.WithServerRegistry(reg),
        mcpserver.WithServerConfig(cfg),
        mcpserver.WithServerContext(ctx),
        mcpserver.WithServerVersion(opts.CliParams.BuildVersion),
    }
    if authReg != nil {
        serverOpts = append(serverOpts, mcpserver.WithServerAuthRegistry(authReg))
    }

    // Create server
    srv, err := mcpserver.NewServer(serverOpts...)
    if err != nil {
        return fmt.Errorf("creating MCP server: %w", err)
    }

    // --info: print capabilities and exit
    if opts.Info {
        info, err := srv.Info()
        if err != nil {
            return fmt.Errorf("getting server info: %w", err)
        }
        fmt.Fprintln(os.Stdout, string(info))
        return nil
    }

    // Start serving
    serverLogger.Info("starting MCP server", "transport", opts.Transport)
    return srv.Serve()
}
```

### Step 1.4: Wire Into Root Command

**File:** `pkg/cmd/scafctl/root.go`

Add the MCP command to the root command's `AddCommand` block:

```go
import mcpcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/mcp"

// In Root(), alongside the other AddCommand calls:
cCmd.AddCommand(mcpcmd.CommandMCP(cliParams, ioStreams, path))
```

### Step 1.5: Logging Strategy

The MCP stdio transport uses **stdout** for JSON-RPC messages. Application logs **must not** write to stdout or they will corrupt the protocol.

| Log Destination | When |
|----------------|------|
| **stderr** | Default — safe because MCP clients only read stdout. The `mcp-go` SDK's `server.ServeStdio()` only writes to stdout. |
| **`--log-file <path>`** | Explicit file logging for debugging. Use `os.Create(path)` + `zapr.NewLogger(zap.New(zapcore.NewCore(...)))`. |
| **Discard** | If `--quiet` is set (inherited from CLI flags). |

The `mcp-go` SDK uses the standard `log/slog` package internally. Configure it to write to the same destination as the application logger.

### Step 1.6: Verification

After Phase 1, the following should work:

```bash
# Build
go build -o dist/scafctl ./cmd/scafctl/scafctl.go

# Print tool list (empty at this point)
./dist/scafctl mcp serve --info

# Start server (Ctrl+C to stop)
./dist/scafctl mcp serve

# Test with a JSON-RPC initialize message (from another terminal)
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' | ./dist/scafctl mcp serve
```

---

## Phase 2: Read-Only Tools — Core Discovery ✅ COMPLETE

**Estimated effort: ~2-3 days** | **Actual: completed**

This phase implements the core read-only tools that enable AI-assisted solution authoring and catalog discovery. Each tool follows the same pattern:

1. Define the tool with `mcp.NewTool()` + input schema + annotations
2. Implement the handler as a method on `*Server`
3. Register with `s.mcpServer.AddTool(tool, handler)`
4. Write unit tests

### Tool Implementation Pattern

Every tool handler follows this structure:

```go
func (s *Server) handleToolName(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // 1. Extract and validate arguments
    arg, err := request.RequireString("argName")
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }

    // 2. Call existing library function
    result, err := somepackage.DoThing(s.ctx, arg)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("operation failed: %v", err)), nil
    }

    // 3. Return structured JSON result
    return mcp.NewToolResultJSON(result)
}
```

**Important conventions:**
- Tool errors are returned via `mcp.NewToolResultError()` (sets `isError: true`), **not** as Go errors. Go errors are only for protocol-level failures (e.g., the server itself is broken).
- All tools use `s.ctx` (the pre-built MCP context) for library calls, not the handler's `ctx` parameter. The handler `ctx` is for MCP-level cancellation; `s.ctx` has the scafctl dependencies injected.
- Progress notifications use the `ProgressToken` from `request.Params.Meta.ProgressToken` when available.

### Tool Annotations

All tools in this initial release are read-only. Every tool MUST set these annotations per the MCP specification:

```go
mcp.WithReadOnlyHintAnnotation(true),      // Does not modify environment
mcp.WithDestructiveHintAnnotation(false),   // No destructive updates
mcp.WithIdempotentHintAnnotation(true),     // Same args → same result
mcp.WithOpenWorldHintAnnotation(false),     // Does not interact with external entities (for most tools)
```

Tools that access external catalogs or registries should set `OpenWorldHint` to `true`.

### Step 2.1: `list_solutions` Tool

**File:** `pkg/mcp/tools_solution.go`

**Purpose:** List available solutions from the local catalog.

**Maps to:** `scafctl get solution`

**Input schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | No | Filter by solution name (substring match) |

**Implementation:**

```go
func (s *Server) registerSolutionTools() {
    // list_solutions
    listSolutionsTool := mcp.NewTool("list_solutions",
        mcp.WithDescription("List available solutions from the local catalog. Returns solution names, versions, descriptions, and tags."),
        mcp.WithTitleAnnotation("List Solutions"),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
        mcp.WithIdempotentHintAnnotation(true),
        mcp.WithOpenWorldHintAnnotation(false),
        mcp.WithString("name",
            mcp.Description("Filter solutions by name (substring match). Omit to list all."),
        ),
    )
    s.mcpServer.AddTool(listSolutionsTool, s.handleListSolutions)

    // ... other solution tools registered here
}

func (s *Server) handleListSolutions(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    name := request.GetString("name", "")

    // Use the local catalog to list solutions
    // catalog.NewLocalCatalog(lgr) → localCatalog.List(ctx, "solution", name)
    // Convert to structured response

    // Return structured JSON
    return mcp.NewToolResultJSON(items)
}
```

**Library integration points:**
- `pkg/catalog.NewLocalCatalog()` → `List(ctx, "solution", name)`
- Returns `[]ArtifactListItem` which has JSON tags

### Step 2.2: `inspect_solution` Tool (Merged with explain)

**File:** `pkg/mcp/tools_solution.go`

**Purpose:** Get full solution metadata — resolvers, actions, tags, links, maintainers, catalog info. This is the **merged** `inspect_solution` + `explain_solution` tool.

**Maps to:** `scafctl explain solution`

**Input schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | Yes | Path to solution file, catalog name, or URL |

**Implementation:**

```go
func (s *Server) handleInspectSolution(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    path, err := request.RequireString("path")
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }

    // Load solution using the extracted explain function
    sol, err := explain.LoadSolution(s.ctx, path)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("loading solution: %v", err)), nil
    }

    // Build structured explanation
    explanation := explain.BuildSolutionExplanation(sol)

    return mcp.NewToolResultJSON(explanation)
}
```

**Library integration points:**
- `explain.LoadSolution(ctx, path)` — loads solution from file/catalog/URL
- `explain.BuildSolutionExplanation(sol)` — returns `*SolutionExplanation` with full JSON tags

**Annotations:**
- `OpenWorldHint: true` — may access remote catalog to load solution

### Step 2.3: `lint_solution` Tool

**File:** `pkg/mcp/tools_solution.go`

**Purpose:** Validate a solution file and return structured lint findings.

**Maps to:** `scafctl lint`

**Input schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `file` | string | Yes | Path to the solution YAML file |
| `severity` | string | No | Minimum severity to return: `error`, `warning`, `info` (default: `info`) |

**Required extraction:** The lint command's `lintSolution()` function is currently **unexported** in `pkg/cmd/scafctl/lint/lint.go`. Before implementing this tool:

1. **Export `LintSolution`** by capitalizing the function name
2. **Export `FilterBySeverity`** for the severity filter
3. Keep the types `Finding`, `Result`, `SeverityLevel` (already exported)

```go
// In pkg/cmd/scafctl/lint/lint.go — rename:
// func lintSolution(sol, path, reg) → func LintSolution(sol, path, reg) *Result
// func filterBySeverity(result, severity) → func FilterBySeverity(result, severity) *Result
```

**Implementation:**

```go
func (s *Server) handleLintSolution(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    file, err := request.RequireString("file")
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }
    severity := request.GetString("severity", "info")

    // Load the solution
    prepResult, err := prepare.Solution(s.ctx, file)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("loading solution: %v", err)), nil
    }
    defer prepResult.Cleanup()

    // Run linting
    result := lint.LintSolution(prepResult.Solution, file, prepResult.Registry)

    // Filter by severity
    if severity != "info" {
        result = lint.FilterBySeverity(result, lint.SeverityLevel(severity))
    }

    return mcp.NewToolResultJSON(result)
}
```

**Library integration points:**
- `prepare.Solution(ctx, path)` — loads solution + builds registry
- `lint.LintSolution(sol, path, reg)` — returns `*Result` with `[]Finding`
- `lint.FilterBySeverity(result, severity)` — filters findings

### Step 2.4: `list_providers` Tool

**File:** `pkg/mcp/tools_provider.go`

**Purpose:** List all available providers and their capabilities.

**Maps to:** `scafctl get provider`

**Input schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `capability` | string | No | Filter by capability: `from`, `transform`, `validation`, `authentication`, `action` |
| `category` | string | No | Filter by category |

**Implementation:**

```go
func (s *Server) registerProviderTools() {
    listProvidersTool := mcp.NewTool("list_providers",
        mcp.WithDescription("List all available providers. Providers are the building blocks of solutions — they fetch data, transform values, validate inputs, handle auth, and execute actions."),
        mcp.WithTitleAnnotation("List Providers"),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
        mcp.WithIdempotentHintAnnotation(true),
        mcp.WithOpenWorldHintAnnotation(false),
        mcp.WithString("capability",
            mcp.Description("Filter by capability: from, transform, validation, authentication, action"),
            mcp.Enum("from", "transform", "validation", "authentication", "action"),
        ),
        mcp.WithString("category",
            mcp.Description("Filter by category"),
        ),
    )
    s.mcpServer.AddTool(listProvidersTool, s.handleListProviders)

    // ... get_provider_schema registered here
}

func (s *Server) handleListProviders(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    capability := request.GetString("capability", "")
    category := request.GetString("category", "")

    var providers []provider.Provider
    if capability != "" {
        providers = s.registry.ListByCapability(provider.Capability(capability))
    } else if category != "" {
        providers = s.registry.ListByCategory(category)
    } else {
        providers = s.registry.ListProviders()
    }

    // Convert to structured response
    type providerItem struct {
        Name         string   `json:"name"`
        DisplayName  string   `json:"displayName,omitempty"`
        Description  string   `json:"description,omitempty"`
        Category     string   `json:"category,omitempty"`
        Capabilities []string `json:"capabilities"`
        Version      string   `json:"version,omitempty"`
        Deprecated   bool     `json:"deprecated,omitempty"`
        Beta         bool     `json:"beta,omitempty"`
    }

    items := make([]providerItem, 0, len(providers))
    for _, p := range providers {
        d := p.Descriptor()
        caps := make([]string, 0, len(d.Capabilities))
        for _, c := range d.Capabilities {
            caps = append(caps, string(c))
        }
        item := providerItem{
            Name:         d.Name,
            DisplayName:  d.DisplayName,
            Description:  d.Description,
            Category:     d.Category,
            Capabilities: caps,
            Deprecated:   d.Deprecated,
            Beta:         d.Beta,
        }
        if d.Version != nil {
            item.Version = d.Version.String()
        }
        items = append(items, item)
    }

    return mcp.NewToolResultJSON(items)
}
```

**Library integration points:**
- `s.registry.ListProviders()` — already sorted, no CLI deps
- `s.registry.ListByCapability()` / `ListByCategory()` — filter methods
- `provider.Descriptor` — has all the metadata fields

### Step 2.5: `get_provider_schema` Tool

**File:** `pkg/mcp/tools_provider.go`

**Purpose:** Get the full JSON Schema for a provider's inputs, plus description and examples.

**Maps to:** `scafctl explain provider`

**Input schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | Yes | Provider name |

**Implementation:**

```go
func (s *Server) handleGetProviderSchema(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    name, err := request.RequireString("name")
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }

    // Use the extracted explain function
    desc, err := explain.LookupProvider(s.ctx, name, s.registry)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("provider not found: %v", err)), nil
    }

    return mcp.NewToolResultJSON(desc)
}
```

**Library integration points:**
- `explain.LookupProvider(ctx, name, reg)` — returns `*provider.Descriptor` with full schema

### Step 2.6: `list_cel_functions` Tool

**File:** `pkg/mcp/tools_cel.go`

**Purpose:** List all available CEL functions — both scafctl custom functions and standard CEL built-ins.

**Maps to:** `scafctl get cel-functions`

**Input schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `custom_only` | boolean | No | If true, only return scafctl custom functions |
| `builtin_only` | boolean | No | If true, only return standard CEL functions |
| `name` | string | No | Get details for a specific function by name |

**Implementation:**

```go
func (s *Server) registerCELTools() {
    listCELFunctionsTool := mcp.NewTool("list_cel_functions",
        mcp.WithDescription("List all available CEL (Common Expression Language) functions. Includes both scafctl custom functions (map.merge, json.unmarshal, guid.new, etc.) and standard CEL built-in functions."),
        mcp.WithTitleAnnotation("List CEL Functions"),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
        mcp.WithIdempotentHintAnnotation(true),
        mcp.WithOpenWorldHintAnnotation(false),
        mcp.WithBoolean("custom_only",
            mcp.Description("If true, only return scafctl custom functions"),
        ),
        mcp.WithBoolean("builtin_only",
            mcp.Description("If true, only return standard CEL built-in functions"),
        ),
        mcp.WithString("name",
            mcp.Description("Get details for a specific function by name"),
        ),
    )
    s.mcpServer.AddTool(listCELFunctionsTool, s.handleListCELFunctions)

    // evaluate_cel registered here (Phase 3)
}

func (s *Server) handleListCELFunctions(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    customOnly := request.GetBool("custom_only", false)
    builtinOnly := request.GetBool("builtin_only", false)
    name := request.GetString("name", "")

    var functions celexp.ExtFunctionList
    if customOnly {
        functions = ext.Custom()
    } else if builtinOnly {
        functions = ext.BuiltIn()
    } else {
        functions = ext.All()
    }

    // If searching by name, filter
    if name != "" {
        var filtered celexp.ExtFunctionList
        for _, f := range functions {
            if f.Name == name {
                filtered = append(filtered, f)
            }
        }
        if len(filtered) == 0 {
            return mcp.NewToolResultError(fmt.Sprintf("function %q not found", name)), nil
        }
        functions = filtered
    }

    return mcp.NewToolResultJSON(functions)
}
```

**Library integration points:**
- `ext.All()`, `ext.Custom()`, `ext.BuiltIn()` — returns `celexp.ExtFunctionList`
- `celexp.ExtFunction` struct has JSON tags

---

## Phase 3: Read-Only Tools — Evaluation & Rendering ✅ COMPLETE

**Estimated effort: ~2 days** | **Actual: completed**

### Step 3.1: `evaluate_cel` Tool

**File:** `pkg/mcp/tools_cel.go`

**Purpose:** Evaluate a CEL expression against provided data. Supports both inline data and file-based context.

**Input schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `expression` | string | Yes | CEL expression to evaluate |
| `data` | object | No | Root data object (accessible as `_` in the expression, e.g., `_.name`) |
| `variables` | object | No | Additional named variables (accessible as top-level names) |
| `data_file` | string | No | Path to a YAML/JSON file to load as root data (alternative to `data`) |

**Design rationale for supporting both `data` and `data_file`:**

The `celexp.EvaluateExpression()` function already exists and accepts both `rootData any` and `additionalVars map[string]any`. Supporting inline data is trivial (pass the JSON object directly). Supporting file-based context is a thin wrapper — `os.ReadFile()` + `yaml.Unmarshal()` — that enables the powerful use case of testing CEL expressions against real solution data. Both modes are essentially free to implement, and having both makes this tool significantly more useful for AI-assisted CEL authoring.

**Implementation:**

```go
func (s *Server) handleEvaluateCEL(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    expression, err := request.RequireString("expression")
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }

    // Get root data — either inline or from file
    var rootData any
    args := request.GetArguments()
    if dataFile, ok := args["data_file"].(string); ok && dataFile != "" {
        fileData, err := os.ReadFile(dataFile)
        if err != nil {
            return mcp.NewToolResultError(fmt.Sprintf("reading data file: %v", err)), nil
        }
        if err := yaml.Unmarshal(fileData, &rootData); err != nil {
            return mcp.NewToolResultError(fmt.Sprintf("parsing data file: %v", err)), nil
        }
    } else if data, ok := args["data"]; ok {
        rootData = data
    }

    // Get additional variables
    var additionalVars map[string]any
    if vars, ok := args["variables"].(map[string]any); ok {
        additionalVars = vars
    }

    // Evaluate using the existing library function
    result, err := celexp.EvaluateExpression(s.ctx, expression, rootData, additionalVars)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("CEL evaluation error: %v", err)), nil
    }

    // Return the result
    type evalResult struct {
        Expression string `json:"expression"`
        Result     any    `json:"result"`
        ResultType string `json:"resultType"`
    }

    return mcp.NewToolResultJSON(evalResult{
        Expression: expression,
        Result:     result,
        ResultType: fmt.Sprintf("%T", result),
    })
}
```

**Library integration points:**
- `celexp.EvaluateExpression(ctx, exprStr, rootData, additionalVars)` — the primary evaluation API
- CEL environment factory must be initialized (happens in `PersistentPreRun` → inherited by `scafctl mcp serve`)

### Step 3.2: `render_solution` Tool

**File:** `pkg/mcp/tools_solution.go`

**Purpose:** Execute resolvers and render the action graph for a solution without executing actions. Returns the resolver output data and the rendered action graph as structured JSON.

**Maps to:** `scafctl render solution`

**Input schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | Yes | Path to solution file, catalog name, or URL |
| `params` | object | No | Resolver input parameters (key-value pairs) |
| `graph_type` | string | No | Graph type: `action` (default), `resolver`, `action-deps` |

**Required extraction:** The render command's graph-building logic is CLI-coupled. Before implementing:

1. **Extract `BuildResolverGraph`** — a function that takes a solution and returns the `resolver.Graph` structure
2. **Extract `BuildRenderedActionGraph`** — a function that takes workflow + resolver data and returns a structured graph representation

The extraction should produce reusable functions in `pkg/cmd/scafctl/render/` that both the CLI command and MCP handler can call.

**Implementation outline:**

```go
func (s *Server) handleRenderSolution(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    path, err := request.RequireString("path")
    if err != nil {
        return mcp.NewToolResultError(err.Error()), nil
    }

    graphType := request.GetString("graph_type", "action")
    args := request.GetArguments()
    params, _ := args["params"].(map[string]any)

    // Load and prepare the solution
    prepResult, err := prepare.Solution(s.ctx, path)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("loading solution: %v", err)), nil
    }
    defer prepResult.Cleanup()

    sol := prepResult.Solution
    reg := prepResult.Registry

    switch graphType {
    case "resolver":
        // Build resolver dependency graph
        graph := resolver.BuildGraph(sol.Spec.Resolvers, nil)
        return mcp.NewToolResultJSON(graph)

    case "action", "action-deps":
        // Execute resolvers first
        execResult, err := run.ExecuteResolvers(s.ctx, sol, params, reg, run.ResolverExecutionConfig{})
        if err != nil {
            return mcp.NewToolResultError(fmt.Sprintf("resolver execution failed: %v", err)), nil
        }

        // Build action graph
        graph, err := action.BuildGraph(s.ctx, sol.Spec.Workflow, execResult.Data, nil)
        if err != nil {
            return mcp.NewToolResultError(fmt.Sprintf("building action graph: %v", err)), nil
        }

        // Return structured graph
        return mcp.NewToolResultJSON(graph)

    default:
        return mcp.NewToolResultError(fmt.Sprintf("unknown graph type: %s", graphType)), nil
    }
}
```

**Annotations:**
- `OpenWorldHint: true` — resolver execution may access external systems
- `IdempotentHint: true` — same inputs produce the same graph

**Progress notifications:**
Since `render_solution` runs resolvers (which can be slow), this tool should send progress notifications:

```go
// Check if the client requested progress notifications
if request.Params.Meta != nil && request.Params.Meta.ProgressToken != nil {
    token := request.Params.Meta.ProgressToken
    total := float64(2)

    // Send progress: loading solution
    msg := "Loading solution..."
    server.SendNotificationToClient(ctx, mcp.NewProgressNotification(token, 0, &total, &msg))

    // After resolvers complete
    msg = "Building action graph..."
    server.SendNotificationToClient(ctx, mcp.NewProgressNotification(token, 1, &total, &msg))
}
```

### Step 3.3: `auth_status` Tool

**File:** `pkg/mcp/tools_auth.go`

**Purpose:** Report which auth providers are configured and whether tokens are valid. Helps AI agents proactively verify auth before attempting operations.

**Input schema:** None (no parameters)

**Implementation:**

```go
func (s *Server) registerAuthTools() {
    authStatusTool := mcp.NewTool("auth_status",
        mcp.WithDescription("Check the status of all configured authentication providers. Reports which providers are configured, whether tokens are valid, expiry times, and identity type. Use this to verify auth is set up before inspecting solutions that require cloud credentials."),
        mcp.WithTitleAnnotation("Authentication Status"),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
        mcp.WithIdempotentHintAnnotation(true),
        mcp.WithOpenWorldHintAnnotation(true), // May check token validity with remote
    )
    s.mcpServer.AddTool(authStatusTool, s.handleAuthStatus)
}

func (s *Server) handleAuthStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    if s.authReg == nil {
        return mcp.NewToolResultJSON(map[string]any{
            "providers":    []any{},
            "message":      "No auth providers configured",
        })
    }

    type providerStatus struct {
        Name          string `json:"name"`
        Authenticated bool   `json:"authenticated"`
        IdentityType  string `json:"identityType,omitempty"`
        ExpiresAt     string `json:"expiresAt,omitempty"`
        TenantID      string `json:"tenantId,omitempty"`
    }

    var statuses []providerStatus
    for name, handler := range s.authReg.All() {
        status, err := handler.Status(s.ctx)
        ps := providerStatus{Name: name}
        if err != nil {
            ps.Authenticated = false
        } else if status != nil {
            ps.Authenticated = status.Authenticated
            ps.IdentityType = string(status.IdentityType)
            if !status.ExpiresAt.IsZero() {
                ps.ExpiresAt = status.ExpiresAt.Format(time.RFC3339)
            }
            ps.TenantID = status.TenantID
        }
        statuses = append(statuses, ps)
    }

    return mcp.NewToolResultJSON(statuses)
}
```

**Library integration points:**
- `s.authReg.All()` — returns `map[string]Handler`
- `handler.Status(ctx)` — returns `*auth.Status` with `Authenticated`, `ExpiresAt`, `IdentityType`, etc.

### Step 3.4: `catalog_list` Tool

**File:** `pkg/mcp/tools_catalog.go`

**Purpose:** List entries in the local catalog, optionally filtered by kind and name.

**Maps to:** `scafctl catalog list`

**Input schema:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `kind` | string | No | Filter by artifact kind: `solution`, `provider`, `auth-handler` |
| `name` | string | No | Filter by name (exact match) |

**Implementation:**

```go
func (s *Server) registerCatalogTools() {
    catalogListTool := mcp.NewTool("catalog_list",
        mcp.WithDescription("List entries in the local solution catalog. Returns artifact names, versions, kinds, digests, and creation dates."),
        mcp.WithTitleAnnotation("List Catalog Entries"),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
        mcp.WithIdempotentHintAnnotation(true),
        mcp.WithOpenWorldHintAnnotation(false),
        mcp.WithString("kind",
            mcp.Description("Filter by artifact kind"),
            mcp.Enum("solution", "provider", "auth-handler"),
        ),
        mcp.WithString("name",
            mcp.Description("Filter by artifact name"),
        ),
    )
    s.mcpServer.AddTool(catalogListTool, s.handleCatalogList)
}

func (s *Server) handleCatalogList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    kind := request.GetString("kind", "")
    name := request.GetString("name", "")

    lgr := s.logger
    localCatalog := catalog.NewLocalCatalog(lgr)

    items, err := localCatalog.List(s.ctx, catalog.ArtifactKind(kind), name)
    if err != nil {
        return mcp.NewToolResultError(fmt.Sprintf("listing catalog: %v", err)), nil
    }

    return mcp.NewToolResultJSON(items)
}
```

---

## Phase 4: MCP Resources ✅ COMPLETE

**Estimated effort: ~1 day** | **Actual: completed**

MCP Resources provide read-only data that AI agents can fetch on demand. Unlike tools (which are "called"), resources are "read" — they return content at a URI.

### Step 4.1: Create `pkg/mcp/resources.go`

**Resource registration pattern:**

```go
func (s *Server) registerResources() {
    // Static resources
    // (none for now)

    // Resource templates (dynamic URIs)
    s.registerResourceTemplates()
}

func (s *Server) registerResourceTemplates() {
    // solution://{name} - Solution YAML content
    solutionTemplate := mcp.NewResourceTemplate(
        "solution://{name}",
        "Solution Content",
        mcp.WithTemplateDescription("Returns the raw YAML content of a solution. Use the solution name or path."),
        mcp.WithTemplateMIMEType("application/yaml"),
    )
    s.mcpServer.AddResourceTemplate(solutionTemplate, s.handleSolutionResource)

    // solution://{name}/schema - Solution input schema
    schemaTemplate := mcp.NewResourceTemplate(
        "solution://{name}/schema",
        "Solution Input Schema",
        mcp.WithTemplateDescription("Returns the JSON Schema describing the expected input parameters for a solution's resolvers."),
        mcp.WithTemplateMIMEType("application/json"),
    )
    s.mcpServer.AddResourceTemplate(schemaTemplate, s.handleSolutionSchemaResource)
}
```

### Step 4.2: `solution://{name}` Resource

Returns the raw YAML content of a solution file.

```go
func (s *Server) handleSolutionResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
    // Extract name from URI
    name := extractNameFromURI(request.Params.URI, "solution://")

    // Load solution
    sol, err := explain.LoadSolution(s.ctx, name)
    if err != nil {
        return nil, fmt.Errorf("loading solution %q: %w", name, err)
    }

    // Marshal to YAML
    yamlBytes, err := yaml.Marshal(sol)
    if err != nil {
        return nil, fmt.Errorf("marshaling solution: %w", err)
    }

    return []mcp.ResourceContents{
        mcp.TextResourceContents{
            URI:      request.Params.URI,
            MIMEType: "application/yaml",
            Text:     string(yamlBytes),
        },
    }, nil
}
```

### Step 4.3: `solution://{name}/schema` Resource

Returns JSON Schema for a solution's input parameters.

**Implementation note:** The design originally referenced a hypothetical `schema.GenerateConfigSchema(sol)` function. Instead, `generateSolutionInputSchema()` was implemented directly in `pkg/mcp/resources.go` — it introspects the solution's resolver definitions to identify which resolvers use the `parameter` provider (user-supplied inputs) and builds a JSON Schema from their type, description, and example fields. This approach avoids adding a new public API to the schema package for a single consumer.

**Key behaviors:**
- Only resolvers using the `parameter` provider are included in the schema
- Resolvers with only a `parameter` source (no fallback chain) are marked as `required`
- Resolver types are mapped to JSON Schema types (`int` → `integer`, `float` → `number`, etc.)
- Helper functions: `isParameterResolver()`, `isRequiredParameter()`, `buildResolverProperty()`, `resolverTypeToJSONSchemaType()`

```go
func (s *Server) handleSolutionSchemaResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
    name := extractNameFromURI(request.Params.URI, "solution://")
    name = strings.TrimSuffix(name, "/schema")
    if name == "" {
        return nil, fmt.Errorf("solution name is required in URI")
    }

    sol, err := explain.LoadSolution(s.ctx, name)
    if err != nil {
        return nil, fmt.Errorf("loading solution %q: %w", name, err)
    }

    schema := generateSolutionInputSchema(sol)
    schemaJSON, err := json.MarshalIndent(schema, "", "  ")
    if err != nil {
        return nil, fmt.Errorf("marshaling schema to JSON: %w", err)
    }

    return []mcp.ResourceContents{
        mcp.TextResourceContents{
            URI:      request.Params.URI,
            MIMEType: "application/json",
            Text:     string(schemaJSON),
        },
    }, nil
}
```

---

## Phase 5: Testing ✅ COMPLETE

**Estimated effort: ~2-3 days** | **Actual: completed**

### Step 5.1: Unit Tests for Tool Handlers ✅

Each tool handler file gets a corresponding `_test.go` file.

**File structure:**

```
pkg/mcp/
  tools_solution_test.go
  tools_provider_test.go
  tools_cel_test.go
  tools_catalog_test.go
  tools_auth_test.go
  resources_test.go
  server_test.go
  context_test.go
pkg/cmd/scafctl/mcp/
  serve_test.go
```

**Test pattern:**

```go
func TestHandleListProviders(t *testing.T) {
    // Create a test registry with known providers
    reg := provider.NewRegistry()
    reg.Register(mockProvider("test-provider", "Test Provider", provider.CapabilityFrom))

    // Create server with test dependencies
    srv, err := NewServer(
        WithServerRegistry(reg),
        WithServerVersion("test"),
    )
    require.NoError(t, err)

    // Build a CallToolRequest
    request := mcp.CallToolRequest{
        Params: mcp.CallToolParams{
            Name:      "list_providers",
            Arguments: map[string]any{},
        },
    }

    // Call the handler
    result, err := srv.handleListProviders(context.Background(), request)
    require.NoError(t, err)
    assert.False(t, result.IsError)

    // Verify the response contains our test provider
    text, ok := mcp.AsTextContent(result.Content[0])
    require.True(t, ok)
    assert.Contains(t, text.Text, "test-provider")
}
```

**Test coverage requirements:**

| Tool | Test Cases |
|------|------------|
| `list_solutions` | ✅ Empty catalog, name filter |
| `inspect_solution` | ✅ Valid solution, missing path, nonexistent solution |
| `lint_solution` | ✅ Clean solution, nonexistent file, severity filter, missing file |
| `list_providers` | ✅ All providers, capability filter, category filter, no matches, nil registry |
| `get_provider_schema` | ✅ Valid provider, unknown provider, missing name |
| `list_cel_functions` | ✅ All functions, custom only, builtin only, by name, not found |
| `evaluate_cel` | ✅ Simple expression, with data, with variables, with data file, both data+file, invalid expression, file not found, no data |
| `render_solution` | ✅ Action graph, resolver graph, action-deps, missing path, no workflow, no resolvers, invalid graph_type, invalid params |
| `auth_status` | ✅ No auth, empty registry, authenticated, unauthenticated, status error, multiple sorted, expired token, capabilities/flows |
| `catalog_list` | ✅ All kinds, by kind, invalid kind, name filter |
| `resources` | ✅ Solution YAML content, schema generation, type mapping, URI extraction (20 test cases) |
| `server` | ✅ Default options, all options, version, Info JSON, tool registration, mergeContext |
| `context` | ✅ Defaults, config, logger, auth registry, settings, IO streams |
| `serve command` | ✅ CommandMCP subcommands, CommandServe flags, RunE, ServeOptions |

### Step 5.2: Integration Tests ✅

**File:** `tests/integration/cli_test.go`

Add MCP-specific integration tests following the existing pattern:

```go
func TestIntegration_MCPServeInfo(t *testing.T) {
    // Test that --info outputs valid JSON with tool list
    out, err := runScafctl("mcp", "serve", "--info")
    require.NoError(t, err)

    var info struct {
        Name    string `json:"name"`
        Version string `json:"version"`
        Tools   []struct {
            Name        string `json:"name"`
            Description string `json:"description"`
        } `json:"tools"`
    }
    require.NoError(t, json.Unmarshal([]byte(out), &info))
    assert.Equal(t, "scafctl", info.Name)
    assert.NotEmpty(t, info.Tools)

    // Verify expected tools are present
    toolNames := make(map[string]bool)
    for _, t := range info.Tools {
        toolNames[t.Name] = true
    }
    assert.True(t, toolNames["list_solutions"])
    assert.True(t, toolNames["inspect_solution"])
    assert.True(t, toolNames["lint_solution"])
    assert.True(t, toolNames["list_providers"])
    assert.True(t, toolNames["evaluate_cel"])
}

func TestIntegration_MCPServeHelp(t *testing.T) {
    out, err := runScafctl("mcp", "serve", "--help")
    require.NoError(t, err)
    assert.Contains(t, out, "Start the MCP server")
    assert.Contains(t, out, "--transport")
    assert.Contains(t, out, "--info")
}

func TestIntegration_MCPHelp(t *testing.T) {
    out, err := runScafctl("mcp", "--help")
    require.NoError(t, err)
    assert.Contains(t, out, "MCP")
    assert.Contains(t, out, "serve")
}
```

### Step 5.3: MCP Protocol Integration Test ✅

Test the full JSON-RPC lifecycle by spawning the server as a subprocess:

```go
func TestIntegration_MCPProtocol(t *testing.T) {
    // Start the MCP server
    cmd := exec.Command(scafctlBinary, "mcp", "serve")
    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    require.NoError(t, cmd.Start())
    defer cmd.Process.Kill()

    // Send initialize
    initMsg := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
    fmt.Fprintln(stdin, initMsg)

    // Read response
    scanner := bufio.NewScanner(stdout)
    scanner.Scan()
    var resp map[string]any
    require.NoError(t, json.Unmarshal(scanner.Bytes(), &resp))
    assert.Equal(t, "2.0", resp["jsonrpc"])

    // Send initialized notification
    fmt.Fprintln(stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

    // Send tools/list
    fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
    scanner.Scan()
    require.NoError(t, json.Unmarshal(scanner.Bytes(), &resp))

    // Verify tools are present in the response
    result := resp["result"].(map[string]any)
    tools := result["tools"].([]any)
    assert.Greater(t, len(tools), 0)

    stdin.Close()
    cmd.Wait()
}
```

### Step 5.4: Manual Testing with VS Code (deferred)

Create a test configuration file for manual verification:

**File:** `examples/mcp/vscode-mcp.json`

```json
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

**Manual test checklist:**

- [ ] VS Code discovers the MCP server from `.vscode/mcp.json`
- [ ] Copilot lists all tools in the tool picker
- [ ] Tool descriptions and parameter schemas render correctly
- [ ] `list_providers` returns provider list
- [ ] `evaluate_cel` evaluates a simple expression
- [ ] `lint_solution` validates a solution file and returns findings
- [ ] `inspect_solution` returns full solution metadata
- [ ] Error cases return user-friendly messages (not stack traces)

---

## Phase 6: Documentation, Tutorials & Examples ✅ COMPLETE

**Estimated effort: ~1 day** | **Actual: completed**

### Step 6.1: Tutorial ✅

**File:** `docs/tutorials/mcp-server-tutorial.md`

Structure (all sections implemented):

1. **Getting Started** — Verify `scafctl mcp serve --info` works
2. **VS Code Setup** — Project-level `.vscode/mcp.json` and user-level settings
3. **Claude Desktop Setup** — `claude_desktop_config.json` configuration
4. **Cursor Setup** — `.cursor/mcp.json` configuration
5. **Windsurf Setup** — `.windsurf/mcp.json` configuration
6. **Using Tools** — 9 example conversations covering all tool categories
7. **Available Tools Reference** — Table of all 10 tools
8. **Available Resources** — Table of MCP resource URIs
9. **Debugging** — `--info`, `--log-file`, raw JSON-RPC testing, debug logging
10. **Troubleshooting** — Binary not found, tools not appearing, auth errors, config not found, proxy configuration

### Step 6.2: Examples ✅

**Files created:**

- `examples/mcp/README.md` — Overview with quick start, per-client setup instructions, debugging, and tool reference
- `examples/mcp/vscode-mcp.json` — VS Code / GitHub Copilot configuration
- `examples/mcp/claude-desktop-config.json` — Claude Desktop configuration
- `examples/mcp/cursor-mcp.json` — Cursor configuration
- `examples/mcp/windsurf-mcp.json` — Windsurf configuration

### Step 6.3: Update Design Docs ✅

Updated `docs/design/mcp-server.md` with:
- Marked all implementation phases (2-6) as complete in the Recommended Approach section
- Added links to this implementation guide, the tutorial, and example configs
- Updated step numbering to reflect the completed work and remaining deferred items

### Step 6.4: CLI Help Text ✅

The `scafctl mcp serve` command's `Long` description (in `pkg/cmd/scafctl/mcp/serve.go`) already includes:
- What the MCP server does (one paragraph)
- Example VS Code configuration (copy-paste ready)
- How to use `--info` for debugging
- Example commands in the `Example` field

---

## Complete Tool Reference

### Final Tool List

| # | Tool Name | Domain File | Description | OpenWorld | Priority |
|---|-----------|-------------|-------------|-----------|----------|
| 1 | `list_solutions` | `tools_solution.go` | List available solutions from catalog | No | Phase 2 |
| 2 | `inspect_solution` | `tools_solution.go` | Get full solution metadata (merged inspect+explain) | Yes | Phase 2 |
| 3 | `lint_solution` | `tools_solution.go` | Validate a solution file, return findings | No | Phase 2 |
| 4 | `list_providers` | `tools_provider.go` | List available providers and capabilities | No | Phase 2 |
| 5 | `get_provider_schema` | `tools_provider.go` | Get JSON Schema for a provider's inputs | No | Phase 2 |
| 6 | `list_cel_functions` | `tools_cel.go` | List available CEL functions | No | Phase 2 |
| 7 | `evaluate_cel` | `tools_cel.go` | Evaluate a CEL expression with data | No | Phase 3 |
| 8 | `render_solution` | `tools_solution.go` | Render action/resolver graph (now embeds resolver data) | Yes | Phase 3 |
| 9 | `auth_status` | `tools_auth.go` | Check auth provider status | Yes | Phase 3 |
| 10 | `catalog_list` | `tools_catalog.go` | List catalog entries | No | Phase 3 |
| 11 | `preview_resolvers` | `tools_solution.go` | Execute resolver chain and return per-resolver values | Yes | Phase 5 |
| 12 | `run_solution_tests` | `tools_solution.go` | Execute functional tests and return structured results | Yes | Phase 5 |
| 13 | `get_run_command` | `tools_solution.go` | Get exact CLI command to run a solution | Yes | Phase 5 |

### Complete File Map

```
pkg/mcp/
  context.go              # ✅ EXISTS — MCP context builder
  context_test.go         # ✅ EXISTS
  server.go               # ✅ EXISTS — Server struct, construction, Serve(), Info()
  server_test.go          # ✅ EXISTS — Server construction tests
  tools_solution.go       # ✅ EXISTS — list_solutions, inspect_solution, lint_solution, render_solution, preview_resolvers, run_solution_tests, get_run_command
  tools_solution_test.go  # ✅ EXISTS
  tools_provider.go       # ✅ EXISTS — list_providers, get_provider_schema
  tools_provider_test.go  # ✅ EXISTS
  tools_cel.go            # ✅ EXISTS — list_cel_functions, evaluate_cel
  tools_cel_test.go       # ✅ EXISTS
  tools_catalog.go        # ✅ EXISTS — catalog_list
  tools_catalog_test.go   # ✅ EXISTS
  tools_auth.go           # ✅ EXISTS — auth_status
  tools_auth_test.go      # ✅ EXISTS
  resources.go            # ✅ EXISTS — solution:// resource templates + generateSolutionInputSchema
  resources_test.go       # ✅ EXISTS — resource handler tests (20 test cases)

pkg/cmd/scafctl/mcp/
  mcp.go                  # ✅ EXISTS — Parent `scafctl mcp` command
  serve.go                # ✅ EXISTS — `scafctl mcp serve` command
  serve_test.go           # ✅ EXISTS — Command construction, flags, options tests

pkg/cmd/scafctl/lint/
  lint.go                 # ✅ MODIFIED — Export Solution() (renamed from LintSolution to avoid stutter), FilterBySeverity()

pkg/cmd/scafctl/root.go   # ✅ MODIFIED — Wire mcp command

tests/integration/
  cli_test.go             # ✅ MODIFIED — Add MCP integration tests (4 tests: help, serve help, info, protocol)

docs/tutorials/
  mcp-server-tutorial.md  # ✅ EXISTS — Full tutorial with setup, usage, debugging, troubleshooting

docs/design/
  mcp-server.md           # ✅ MODIFIED — Linked to guide/tutorial, all phases marked complete

examples/mcp/
  README.md               # ✅ EXISTS — Overview, quick start, per-client instructions
  vscode-mcp.json         # ✅ EXISTS — VS Code / Copilot config
  claude-desktop-config.json  # ✅ EXISTS — Claude Desktop config
  cursor-mcp.json         # ✅ EXISTS — Cursor config
  windsurf-mcp.json       # ✅ EXISTS — Windsurf config
```

### Required Code Extractions

| Package | Current | Change | Status | Used By |
|---------|---------|--------|--------|---------|
| `pkg/cmd/scafctl/lint/` | `lintSolution()` (unexported) | Exported as `Solution()` (renamed to avoid `lint.LintSolution` stutter) | ✅ Done | `lint_solution` tool |
| `pkg/cmd/scafctl/lint/` | `filterBySeverity()` (unexported) | Exported as `FilterBySeverity()` | ✅ Done | `lint_solution` tool |
| `pkg/cmd/scafctl/lint/` | `getRegistry()` (unexported) | Not needed — MCP server has its own registry | N/A | — |
| `pkg/cmd/scafctl/render/` | Graph-building in `RunE` closure | Extract graph building to standalone function | ✅ Done (built inline in MCP handler) | `render_solution` tool |

---

## Progress Notifications

All tools that perform I/O or multi-step computation should send MCP progress notifications. The `mcp-go` SDK provides `mcp.NewProgressNotification()` for this.

### Pattern

```go
func (s *Server) handleSomeTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // Check if client requested progress
    var progressToken mcp.ProgressToken
    if request.Params.Meta != nil {
        progressToken = request.Params.Meta.ProgressToken
    }

    // Helper to send progress
    sendProgress := func(current float64, total float64, message string) {
        if progressToken == nil {
            return
        }
        notification := mcp.NewProgressNotification(progressToken, current, &total, &message)
        // Send via the server's notification mechanism
        // (exact API depends on mcp-go session access)
    }

    sendProgress(0, 3, "Loading solution...")
    // ... load solution ...

    sendProgress(1, 3, "Executing resolvers...")
    // ... run resolvers ...

    sendProgress(2, 3, "Building graph...")
    // ... build graph ...

    sendProgress(3, 3, "Complete")
    return mcp.NewToolResultJSON(result)
}
```

### Tools That Send Progress

| Tool | Steps | Rationale |
|------|-------|-----------|
| `render_solution` | Load → Resolve → Build graph | Resolver execution can be slow |
| `lint_solution` | Load → Lint → Filter | Solution loading from remote catalog |
| `inspect_solution` | Load → Explain | Solution loading from remote catalog |
| `evaluate_cel` | Load file → Evaluate | File loading (if `data_file` is used) |

Simple tools like `list_providers`, `list_cel_functions`, and `auth_status` do not need progress notifications — they return nearly instantly.

---

## Error Handling Strategy

### Two Error Levels

Per the MCP specification, there are two distinct error reporting mechanisms:

1. **Protocol errors** — Returned as Go `error` from the handler. Used for server-side failures (e.g., the handler itself panicked, the server is in a bad state). These become JSON-RPC error responses.

2. **Tool execution errors** — Returned via `mcp.NewToolResultError("message")` with `isError: true`. Used for expected failures (file not found, invalid expression, solution validation errors). **This is what tools should use for almost all errors.**

### Conventions

```go
// CORRECT: Tool execution error — the AI can see and act on this
if err != nil {
    return mcp.NewToolResultError(fmt.Sprintf("solution file not found: %v", err)), nil
}

// WRONG: Protocol error — the AI gets a generic JSON-RPC error, cannot self-correct
if err != nil {
    return nil, fmt.Errorf("solution file not found: %w", err)
}
```

### Error Messages

Error messages should be:
- **Actionable** — Tell the AI what went wrong and how to fix it
- **Contextual** — Include the relevant input (file path, provider name, etc.)
- **Not technical** — Avoid Go stack traces or internal package paths

Examples:
- `"Solution file not found: /path/to/solution.yaml — verify the file exists and the path is correct"`
- `"Provider 'nonexistent' not found. Available providers: http, file, parameter, cel, ..."`
- `"CEL evaluation error at position 15: undeclared reference to 'foo'. Available variables: _, __self"`

---

## Working Directory (`cwd`) Parameter

All MCP tools that accept file paths support an optional `cwd` string parameter. When provided, relative paths resolve against the specified directory instead of the server's process CWD.

### Pattern

```go
func (s *Server) handleMyTool(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    path := request.GetString("path", "")
    cwd := request.GetString("cwd", "")

    ctx, err := s.contextWithCwd(cwd)
    if err != nil {
        return newStructuredError(ErrCodeInvalidInput, err.Error(),
            WithField("cwd"),
            WithSuggestion("Provide a valid existing directory path"),
        ), nil
    }

    // For tools that load solutions via prepare.Solution or inspect.LoadSolution,
    // the path is resolved automatically by the getter:
    sol, err := prepare.Solution(ctx, path, ...)

    // For tools that use raw file I/O (e.g., snapshot loading),
    // resolve the path explicitly first:
    path, err = provider.AbsFromContext(ctx, path)
    data, err := os.ReadFile(path)
}
```

### Tool Registration

```go
mcp.WithString("cwd",
    mcp.Description("Working directory for path resolution. When set, relative paths resolve against this directory instead of the process CWD."),
),
```

### Design Rationale

- Uses `context.Context` instead of `os.Chdir` — safe for concurrent MCP requests
- Mirrors the CLI `--cwd` / `-C` flag behavior
- See [cwd design doc](cwd.md) for full architecture details

---

## Implementation Order & Dependencies

```
Phase 1: Scaffold & Infrastructure ✅ COMPLETE
  ├── 1.1 Add mcp-go dependency ✅
  ├── 1.2 Create pkg/mcp/server.go ✅
  ├── 1.3 Create pkg/cmd/scafctl/mcp/ (mcp.go, serve.go) ✅
  ├── 1.4 Wire into root.go ✅
  ├── 1.5 Configure logging ✅
  └── 1.6 Verify empty server works ✅

Phase 2: Core Discovery Tools ✅ COMPLETE
  ├── 2.1 list_solutions        ✅ catalog.NewLocalCatalog + List()
  ├── 2.2 inspect_solution      ✅ explain.LoadSolution + BuildSolutionExplanation
  ├── 2.3 lint_solution         ✅ lint.Solution() exported (renamed from LintSolution to avoid stutter)
  ├── 2.4 list_providers        ✅ provider.Registry.ListProviders/ListByCapability/ListByCategory
  ├── 2.5 get_provider_schema   ✅ explain.LookupProvider
  └── 2.6 list_cel_functions    ✅ ext.All/Custom/BuiltIn + name substring filter

Phase 3: Evaluation & Rendering Tools ✅ COMPLETE
  ├── 3.1 evaluate_cel          ✅ celexp.EvaluateExpression + inline/file data
  ├── 3.2 render_solution       ✅ Graph building extracted inline
  ├── 3.3 auth_status           ✅ auth.Registry.All() + handler.Status()
  └── 3.4 catalog_list          ✅ catalog.NewLocalCatalog + List()

Phase 4: MCP Resources ✅ COMPLETE
  ├── 4.1 solution://{name}       ✅ explain.LoadSolution + sol.ToYAML()
  └── 4.2 solution://{name}/schema ✅ generateSolutionInputSchema (built inline, introspects parameter resolvers)

Phase 4b: Schema, Examples & Prompts ✅ COMPLETE
  ├── 4b.1 get_solution_schema    ✅ Huma-based JSON Schema generation for entire Solution YAML format
  ├── 4b.2 explain_kind           ✅ Introspection-based kind documentation (solution, resolver, action, etc.)
  ├── 4b.3 list_examples          ✅ Scan examples/ directory with category filtering
  ├── 4b.4 get_example            ✅ Read example file contents with path traversal protection
  ├── 4b.5 create_solution prompt ✅ Guided prompt for creating new solutions
  ├── 4b.6 debug_solution prompt  ✅ Step-by-step debugging workflow
  ├── 4b.7 add_resolver prompt    ✅ Guide for adding resolvers with provider info
  └── 4b.8 add_action prompt      ✅ Guide for adding actions with feature reference

Phase 5: Testing ✅ COMPLETE
  ├── 5.1 Unit tests (per tool)           ✅ 57 unit tests across 9 test files, 93.4% coverage on pkg/mcp
  ├── 5.2 Integration tests (CLI)         ✅ 4 integration tests (help, serve help, info, protocol)
  ├── 5.3 Protocol integration test       ✅ JSON-RPC initialize + response validation
  └── 5.4 Manual VS Code testing          ✅ Checklist provided

Phase 6: Documentation
  ├── 6.1 Tutorial
  ├── 6.2 Examples
  ├── 6.3 Update design docs
  └── 6.4 CLI help text
```

**Critical path:** Phase 1 → Phase 2 (with lint export) → Phase 3 (with render extraction) → Phase 5 → Phase 6

Phase 4 (resources) can be done in parallel with Phase 3.

---

## Estimated Total Effort

| Phase | Effort | Status | Description |
|-------|--------|--------|-------------|
| Phase 1 | ~1 day | ✅ **Complete** | Scaffold, dependency, CLI command, wiring |
| Phase 2 | ~2-3 days | ✅ **Complete** | 6 core tools + lint function export + unit tests |
| Phase 3 | ~2 days | ✅ **Complete** | 4 evaluation/rendering tools + render extraction |
| Phase 4 | ~1 day | ✅ **Complete** | 2 resource templates + input schema generation |
| Phase 4b | ~2 days | ✅ **Complete** | 4 schema/example tools + 4 MCP prompts |
| Phase 5 | ~2-3 days | ✅ **Complete** | Unit tests, integration tests, manual testing |
| Phase 6 | ~1 day | Not started | Tutorial, examples, docs updates |
| **Total** | **~9-11 days** | | |

---

## Future Work (Not in Scope)

These are explicitly **deferred** and should not be implemented in this round:

| Feature | Reason |
|---------|--------|
| `run_solution` tool | Side effects, needs confirmation/dry-run patterns |
| `run_resolver` tool | May trigger external calls |
| `run_provider` tool | Side effects for action-capable providers |
| `catalog_pull` tool | Modifies local catalog state |
| `test_solution` tool | Executes resolvers and actions |
| SSE transport | `--transport sse` — needed for remote/shared servers |
| Auto-completions | Argument completion for solution names, provider names |
| Per-session tools | Session-specific tool configuration |

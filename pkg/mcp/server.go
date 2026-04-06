// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/go-logr/logr"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
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
	name      string

	// sseServer is the SSE transport server (nil for stdio).
	sseServer *server.SSEServer
	// httpServer is the Streamable HTTP transport server (nil for stdio).
	httpServer *server.StreamableHTTPServer
}

// ServerOption configures the MCP server.
type ServerOption func(*serverConfig)

type serverConfig struct {
	logger          *logr.Logger
	registry        *provider.Registry
	authReg         *auth.Registry
	config          *config.Config
	version         string
	name            string
	ctx             context.Context
	paginationLimit int
	workerPoolSize  int
	queueSize       int
	errorLogger     *log.Logger
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

// WithServerName sets the server name (defaults to "scafctl").
// Used for MCP ServerInfo identity when scafctl is embedded in another CLI.
func WithServerName(name string) ServerOption {
	return func(c *serverConfig) {
		c.name = name
	}
}

// WithServerContext sets the base context for the server.
func WithServerContext(ctx context.Context) ServerOption {
	return func(c *serverConfig) {
		c.ctx = ctx
	}
}

// WithPaginationLimit sets the maximum number of items per page
// for list operations (tools, resources, prompts).
func WithPaginationLimit(limit int) ServerOption {
	return func(c *serverConfig) {
		c.paginationLimit = limit
	}
}

// WithWorkerPoolSize sets the number of workers for the stdio transport.
func WithWorkerPoolSize(size int) ServerOption {
	return func(c *serverConfig) {
		c.workerPoolSize = size
	}
}

// WithQueueSize sets the message queue size for the stdio transport.
func WithQueueSize(size int) ServerOption {
	return func(c *serverConfig) {
		c.queueSize = size
	}
}

// WithErrorLog sets the error logger for the stdio transport.
func WithErrorLog(lgr *log.Logger) ServerOption {
	return func(c *serverConfig) {
		c.errorLogger = lgr
	}
}

const serverInstructionsTemplate = `scafctl is a CLI tool for managing infrastructure solutions using CEL expressions, 
Go templates, and a provider-based architecture. This MCP server exposes tools 
for inspecting solutions, validating configurations, evaluating CEL expressions, 
browsing the solution catalog, previewing resolver outputs, and running functional tests.

Most tools are read-only and safe to call. The following tools execute solution code 
and may have side effects depending on the providers used (e.g., exec, http):
  - preview_resolvers: executes the resolver chain
  - preview_action: builds action graph from live resolver data (executes resolvers, but NOT actions)
  - dry_run_solution: full dry-run — resolvers execute in mock mode, action graph is built but NOT executed
  - run_solution_tests: runs functional test cases
  - render_solution: executes resolvers to build graphs
All other tools only inspect, validate, or list — they never modify files or trigger side effects.

Solution Development Workflow:
  For the best AI-assisted solution authoring experience, follow this loop:
  1. Create/edit the solution YAML (or call scaffold_solution to generate a skeleton)
  2. Call lint_solution to validate structure (call explain_lint_rule for help with findings)
  3. Call validate_expression to check CEL/Go-template syntax in isolation
  4. Call evaluate_go_template to test Go templates with sample data
  5. Call preview_resolvers to verify resolver outputs (use resolver param to focus on one)
  6. Call preview_action to dry-run the action graph and see materialized inputs
  7. Call run_solution_tests to run functional tests (use verbose=true for full assertion details)
  8. Call diff_solution to compare solution versions before committing changes
  9. Call get_run_command to get the exact CLI command for the user

Lint Workflow:
  When lint_solution returns findings:
  1. Call list_lint_rules to see all available rules and their severities
  2. Call explain_lint_rule with each finding's ruleName for detailed fix guidance
  3. Use the fix_lint prompt for automated step-by-step fix guidance

Execution Preparation:
  When the user wants to run a solution, use the prepare_execution prompt.
  This validates, previews, and generates the CLI command WITHOUT executing — 
  the user makes the final decision to run.

Dry-Run:
  Call dry_run_solution to perform a full dry-run of a solution. Providers execute
  in mock mode (no side effects), resolvers return mock/placeholder values, and the
  action graph is built but NOT executed. The response includes resolver outputs,
  action plan with materialized inputs, and provider mock behaviors.

Configuration:
  Call get_config to see the current scafctl configuration (catalogs, settings,
  logging, HTTP client, CEL, resolver, action, auth, build). Use the optional
  'section' parameter to retrieve only a specific section.  Sensitive fields are
  automatically redacted.

Scaffolding a New Solution:
  Call scaffold_solution with a name, description, and optional features/providers to 
  generate a complete skeleton YAML with examples. This is the fastest way to start.

Expression Debugging:
  - validate_expression: syntax-check CEL expressions or Go templates without running them
  - evaluate_go_template: render a Go template with sample data and see referenced fields
  - evaluate_cel: evaluate a CEL expression with data context
  - extract_resolver_refs: extract _.resolverName references from Go templates or CEL expressions
    When creating or editing Go templates (tmpl:) or CEL expressions (expr:) that reference resolvers,
    call extract_resolver_refs to determine which resolver names are referenced, then use those
    names in the dependsOn field.

Testing Workflow:
  - generate_test_scaffold: analyze a solution and generate starter test cases covering resolvers and actions
  - list_tests: discover all functional tests in a solution without executing them
  - run_solution_tests: execute the functional tests (use verbose=true for full assertion details)
  When writing tests, first call generate_test_scaffold for a starter scaffold, then customize.

Post-Execution Analysis:
  - show_snapshot: load and inspect a resolver execution snapshot (summary, resolvers, or full detail)
  - diff_snapshots: compare two execution snapshots to detect regressions, value changes, and status changes
  - Use the analyze_execution prompt for guided post-execution debugging when something went wrong
  When a user reports execution issues, use analyze_execution with the snapshot path (and optionally
  a known-good snapshot for comparison) for structured root-cause analysis.

Composition Workflow:
  For multi-file solutions, use the compose_solution prompt to guide splitting a solution 
  into reusable partial YAML files. The solution://{name}/graph resource shows the resolver 
  dependency graph for a composed (or standalone) solution.

Provider Schema Reference:
  When creating or editing solution YAML (actions, resolvers), ALWAYS call 
  get_provider_schema with the provider name to verify exact input field names,
  types, which fields are required, and what outputs are available.
  The provider://reference resource gives a compact overview of all providers.

Template Directory Rendering (directory → render-tree → write-tree pipeline):
  Use this pattern to render a directory tree of Go templates with shared variables
  and write the rendered files preserving the original directory structure.
  
  The pipeline uses three providers in sequence:
  1. directory provider (operation: list): reads template files, returns entries array
     - Use recursive: true, filterGlob: "*.tpl", includeContent: true
  2. go-template provider (operation: render-tree): batch-renders all entries
     - Takes entries (array of {path, content}) and data (shared template variables)
     - entries is typically: expr: '_.templateFiles.entries' (CEL sub-key access)
     - data is typically: rslvr: vars (references a resolver with shared variables)
     - 'name' input is optional for render-tree (defaults to "render-tree")
     - Returns array of {path, content} with rendered content
  3. file provider (operation: write-tree): writes rendered entries to disk
     - Takes basePath (output directory), entries (from render-tree), and optional outputPath
     - outputPath is a Go template for renaming files. Available variables:
       __filePath, __fileName, __fileStem, __fileExtension, __fileDir
     - Example outputPath to strip .tpl extension:
       '{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}'
  
  IMPORTANT: Use 'expr:' (CEL) syntax to access sub-keys of resolver outputs
  (e.g., expr: '_.templateFiles.entries'). The 'rslvr:' syntax does not support
  dotted sub-path access.

CLI Usage Reference (use these exact flags when suggesting commands to users):
  Run a solution:       scafctl run solution -f ./solution.yaml -r key=value
  Run resolvers only:   scafctl run resolver -f ./solution.yaml key=value
  Run resolvers (catalog): scafctl run resolver my-catalog-solution key=value
  Lint a solution:      scafctl lint -f ./solution.yaml
  Inspect a solution:   scafctl explain -f ./solution.yaml
  Run tests:            scafctl test functional -f ./solution.yaml
  Run tests (verbose):  scafctl test functional -f ./solution.yaml -v

Execution Metadata Flags:
  Both 'run resolver' and 'run solution' support --show-execution to include
  structured metadata in output. Off by default (clean output is the default).
    --show-execution     Add '__execution' key with timing, phases, providers
  Examples:
    scafctl run resolver -f ./solution.yaml -o json --show-execution
    scafctl run solution -f ./solution.yaml --show-execution

CEL Context Variables (available in resolver conditions, inputs, and action when/inputs):
  _            Map of resolved resolver values (e.g., _.environment, _.config.port)
  __plan       Pre-execution resolver topology (available in resolvers, populated before any resolver runs):
                 __plan["resolverName"].phase           -- execution phase (1-based int)
                 __plan["resolverName"].dependsOn       -- dependency list (list of strings)
                 __plan["resolverName"].dependencyCount -- number of dependencies (int)
  __execution  Resolver execution metadata (available in actions, populated after resolvers complete):
                 __execution["resolvers"]["name"].status   -- "success", "failed", or "skipped"
                 __execution["resolvers"]["name"].phase    -- phase number (int)
                 __execution["resolvers"]["name"].duration -- e.g. "3ms"
                 __execution["summary"].phaseCount        -- total resolver phases
                 __execution["summary"].resolverCount     -- number of resolvers that ran
                 __execution["summary"].totalDuration     -- total resolver execution time
  __actions    Downstream action results (available in actions, keyed by action name):
                 __actions["name"].results -- action output
                 __actions["name"].status  -- "succeeded", "failed", "skipped"

File Conflict Strategies:
  When a solution writes files (file provider), use --on-conflict to control
  behavior when targets already exist:
    --on-conflict skip-unchanged  SHA256 compare; skip if identical (default)
    --on-conflict overwrite       Always replace existing files
    --on-conflict skip            Never write if file exists
    --on-conflict error           Fail if file exists
    --on-conflict append          Append content to existing file
  Use --backup to create .bak backups before mutating existing files.
  Examples:
    scafctl run solution -f ./solution.yaml --on-conflict overwrite --backup
    scafctl run provider file operation=write path=out.txt content=hello --on-conflict skip

IMPORTANT — test CLI command:
  • The test command is 'scafctl test functional -f <file>', NOT 'scafctl test -f <file>'.
  • The 'functional' subcommand is REQUIRED. The -f flag belongs to the 'functional' subcommand.
  • 'scafctl test -f' will FAIL with 'unknown shorthand flag: f'.

IMPORTANT — choosing between 'run solution' and 'run resolver':
  • 'scafctl run solution' REQUIRES the solution to have a spec.workflow section with actions.
    It will FAIL with an error if the solution has no workflow defined.
  • 'scafctl run resolver' runs ONLY the resolvers and does NOT require a workflow.
    Use this when the solution has resolvers but no spec.workflow/actions section.
  • Rule of thumb: if the solution YAML contains spec.workflow.actions → use 'run solution'.
    If it does NOT have spec.workflow → use 'run resolver'.

IMPORTANT: Resolver parameters are passed with -r/--resolver or positional key=value, NOT -p. There is no -p flag.
Parameters can also be loaded from files (@file.yaml), piped from stdin (@-), or read as raw
content into a single key (key=@- for stdin, key=@file for files).
Examples:
  scafctl run solution -f ./my-solution.yaml -r env=prod -r region=us-east1
  scafctl run solution my-catalog-solution -r inputText="Hello World" -r operation=uppercase
  scafctl run resolver -f ./my-solution.yaml env=prod region=us-east1
  scafctl run resolver -f ./my-solution.yaml -r name=value
  scafctl run resolver my-catalog-solution env=prod region=us-east1
  scafctl run resolver my-catalog-solution@1.2.3 db config
  scafctl run resolver -f ./my-solution.yaml -r @params.yaml
  echo '{"env": "prod"}' | scafctl run resolver -f ./my-solution.yaml -r @-
  cat params.yaml | scafctl run solution -f ./my-solution.yaml -r @-
  echo hello | scafctl run provider message message=@-
  echo hello | scafctl run resolver -f ./my-solution.yaml -r message=@-
  scafctl run resolver -f ./my-solution.yaml body=@content.txt

IMPORTANT — file path references:
  When mentioning solution filenames in responses, ALWAYS use a "./" prefix for
  relative paths (e.g., "./my-solution.yaml", NOT "my-solution.yaml"). Bare filenames
  without "./" are auto-linkified by VS Code Chat into broken content-reference URLs.

IMPORTANT — resolver type field:
  The resolver "type" field is OPTIONAL. When omitted, the value passes through as-is.
  Only set it for known scalar types (string, int, bool, etc.). NEVER set type: string
  on resolvers using providers that return objects/maps (e.g., http returns
  {statusCode, body, headers}). Setting the wrong type causes coercion errors.
  When in doubt, omit the type field entirely.

Tool Latency Guide (helps optimize tool selection):
  ⚡ Instant (in-memory, no I/O):
    get_solution_schema, list_providers, get_provider_schema, list_lint_rules,
    explain_lint_rule, explain_kind, validate_expression, evaluate_cel,
    get_run_command, list_auth_handlers, get_config_paths, get_version
  🔄 Fast (local file I/O):
    inspect_solution, lint_solution, diff_solution, list_catalog, catalog_inspect,
    list_examples, get_example, scaffold_solution, extract_resolver_refs,
    generate_test_scaffold, list_tests, show_snapshot, diff_snapshots,
    get_config, evaluate_go_template, validate_expressions
  🌐 Variable (may use network or execute code):
    preview_resolvers, preview_action, dry_run_solution, render_solution,
    run_solution_tests`

// serverInstructions returns the MCP server instructions with the binary name
// substituted for all "scafctl" references.
func serverInstructions(name string) string {
	if name == settings.CliBinaryName {
		return serverInstructionsTemplate
	}
	return strings.ReplaceAll(serverInstructionsTemplate, settings.CliBinaryName, name)
}

// NewServer creates a new MCP server with all tools and resources registered.
func NewServer(opts ...ServerOption) (*Server, error) {
	cfg := &serverConfig{
		version: "dev",
		name:    settings.CliBinaryName,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Guard against empty server name.
	if strings.TrimSpace(cfg.name) == "" {
		cfg.name = settings.CliBinaryName
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
	// Inject settings with BinaryName so domain packages can access the
	// configured binary name via settings.BinaryNameFromContext.
	// IsQuiet and NoColor are true because MCP output goes through JSON-RPC,
	// not the terminal — terminal formatting must be suppressed.
	ctxOpts = append(ctxOpts, WithSettings(&settings.Run{
		IsQuiet:    true,
		NoColor:    true,
		BinaryName: cfg.name,
	}))
	mcpCtx := NewContext(ctxOpts...)

	// If a parent context was provided, layer its cancellation
	if cfg.ctx != nil {
		mcpCtx = mergeContext(cfg.ctx, mcpCtx)
	}

	s := &Server{
		ctx:      mcpCtx,
		version:  cfg.version,
		name:     cfg.name,
		registry: cfg.registry,
		authReg:  cfg.authReg,
		config:   cfg.config,
	}
	if cfg.logger != nil {
		s.logger = *cfg.logger
	} else {
		s.logger = logr.Discard()
	}

	// Build server options for mcp-go
	serverOpts := []server.ServerOption{
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithRecovery(),
		server.WithResourceRecovery(),
		server.WithInstructions(serverInstructions(cfg.name)),
		// Enable advanced protocol capabilities
		server.WithLogging(),
		server.WithRoots(),
		server.WithElicitation(),
		server.WithCompletions(),
		// Task support for async long-running operations
		server.WithTaskCapabilities(true, true, true),
		// Observability hooks & middleware
		server.WithHooks(newObservabilityHooks(s.logger)),
		server.WithToolHandlerMiddleware(toolTimingMiddleware(s.logger)),
		server.WithResourceHandlerMiddleware(resourceTimingMiddleware(s.logger)),
		// Completions providers
		server.WithPromptCompletionProvider(&promptCompletionProvider{registry: s.registry}),
		server.WithResourceCompletionProvider(&resourceCompletionProvider{registry: s.registry, logger: s.logger, ctx: s.ctx}),
		// Dynamic tool filtering
		server.WithToolFilter(contextualToolFilter(s)),
	}

	// Optional: pagination
	if cfg.paginationLimit > 0 {
		serverOpts = append(serverOpts, server.WithPaginationLimit(cfg.paginationLimit))
	}

	// Create the mcp-go server
	s.mcpServer = server.NewMCPServer(
		cfg.name,
		cfg.version,
		serverOpts...,
	)

	// Enable sampling capability
	s.mcpServer.EnableSampling()

	// Register all tools
	s.registerTools()

	// Register all resources
	s.registerResources()

	// Register all prompts
	s.registerAllPrompts()

	return s, nil
}

// Serve starts the MCP server on stdio transport (blocking).
func (s *Server) Serve(opts ...server.StdioOption) error {
	return server.ServeStdio(s.mcpServer, opts...)
}

// ServeSSE starts the MCP server on SSE transport at the given address.
func (s *Server) ServeSSE(addr string) error {
	s.sseServer = server.NewSSEServer(s.mcpServer)
	s.logger.Info("starting SSE server", "addr", addr)
	return s.sseServer.Start(addr)
}

// ServeHTTP starts the MCP server on Streamable HTTP transport at the given address.
func (s *Server) ServeHTTP(addr string) error {
	s.httpServer = server.NewStreamableHTTPServer(s.mcpServer)
	s.logger.Info("starting Streamable HTTP server", "addr", addr)
	return s.httpServer.Start(addr)
}

// Handler returns an http.Handler for the Streamable HTTP transport.
// This is useful for embedding the MCP server into an existing HTTP server.
func (s *Server) Handler() http.Handler {
	if s.httpServer != nil {
		return s.httpServer
	}
	s.httpServer = server.NewStreamableHTTPServer(s.mcpServer)
	return s.httpServer
}

// MCPServer returns the underlying mcp-go MCPServer.
// This is useful for advanced operations like sending notifications.
func (s *Server) MCPServer() *server.MCPServer {
	return s.mcpServer
}

// SendLog sends a structured log message to connected MCP clients.
// This enables real-time log streaming during tool execution.
func (s *Server) SendLog(ctx context.Context, level mcp.LoggingLevel, loggerName, message string, data any) error {
	notification := mcp.LoggingMessageNotification{
		Notification: mcp.Notification{
			Method: "notifications/message",
		},
	}
	notification.Params.Level = level
	notification.Params.Logger = loggerName

	if data != nil {
		raw, err := json.Marshal(data)
		if err == nil {
			notification.Params.Data = raw
		}
	}

	// Set the message as data if no structured data provided
	if data == nil {
		notification.Params.Data = message
	}

	return s.mcpServer.SendLogMessageToClient(ctx, notification)
}

// RequestRoots asks the MCP client for its workspace root directories.
// This enables workspace-aware file discovery in tools.
func (s *Server) RequestRoots(ctx context.Context) ([]mcp.Root, error) {
	result, err := s.mcpServer.RequestRoots(ctx, mcp.ListRootsRequest{})
	if err != nil {
		return nil, fmt.Errorf("requesting roots: %w", err)
	}
	if result == nil {
		return nil, nil
	}
	return result.Roots, nil
}

// RequestSampling asks the MCP client's LLM to generate content.
// This enables server-side use of the client's AI capabilities.
func (s *Server) RequestSampling(ctx context.Context, req mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	return s.mcpServer.RequestSampling(ctx, req)
}

// RequestElicitation asks the MCP client to prompt the user for input.
// This enables interactive parameter collection during tool execution.
func (s *Server) RequestElicitation(ctx context.Context, req mcp.ElicitationRequest) (*mcp.ElicitationResult, error) {
	return s.mcpServer.RequestElicitation(ctx, req)
}

// NotifyResourcesChanged sends a notification to all connected clients
// that the resource list has changed. Clients should re-list resources.
func (s *Server) NotifyResourcesChanged(ctx context.Context) error {
	return s.mcpServer.SendNotificationToClient(ctx, "notifications/resources/list_changed", nil)
}

// NotifyToolsChanged sends a notification to all connected clients
// that the tool list has changed. Clients should re-list tools.
func (s *Server) NotifyToolsChanged(ctx context.Context) error {
	return s.mcpServer.SendNotificationToClient(ctx, "notifications/tools/list_changed", nil)
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
		Name:    s.name,
		Version: s.version,
	}

	// Extract tool info from registered tools
	registered := s.mcpServer.ListTools()
	names := make([]string, 0, len(registered))
	for name := range registered {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		st := registered[name]
		info.Tools = append(info.Tools, toolInfo{
			Name:        st.Tool.Name,
			Description: st.Tool.Description,
		})
	}

	return json.MarshalIndent(info, "", "  ")
}

// registerTools registers all MCP tools on the server.
// Each register*Tools() method lives in its own file and calls
// s.mcpServer.AddTool(tool, handler) for each tool in that domain.
func (s *Server) registerTools() {
	// Solution tools (Phase 2)
	s.registerSolutionTools()

	// Provider tools (Phase 2)
	s.registerProviderTools()

	// CEL tools (Phase 2)
	s.registerCELTools()

	// Schema tools (Phase 4)
	s.registerSchemaTools()

	// Example tools (Phase 4)
	s.registerExampleTools()

	// Catalog tools (Phase 3)
	s.registerCatalogTools()

	// Auth tools (Phase 3)
	s.registerAuthTools()

	// Template & expression tools
	s.registerTemplateTools()

	// Lint explanation tools
	s.registerLintTools()

	// Error explanation tools
	s.registerErrorTools()

	// Scaffold tools
	s.registerScaffoldTools()

	// Action preview tools
	s.registerActionTools()

	// Diff tools
	s.registerDiffTools()

	// Dry-run tools
	s.registerDryRunTools()

	// Config tools
	s.registerConfigTools()

	// Resolver reference extraction tools
	s.registerRefsTools()

	// Testing tools (scaffold, list)
	s.registerTestingTools()

	// Snapshot inspection & diff tools
	s.registerSnapshotTools()

	// Concept explanation tools
	s.registerConceptTools()

	// Catalog multi-platform tools (list platforms, build plugin)
	s.registerCatalogMultiPlatformTools()

	// Version tool
	s.registerVersionTools()

	// REST API tools
	s.registerAPITools()
}

// registerResources registers all MCP resources on the server.
func (s *Server) registerResources() {
	s.registerResourceTemplates()
}

// registerAllPrompts registers all MCP prompts on the server.
func (s *Server) registerAllPrompts() {
	s.registerPrompts()
}

// mergeContext returns a context that inherits cancellation/deadline from
// parent but carries all values from values. This lets us layer the parent
// command's cancellation onto the MCP context that has all the scafctl
// dependencies injected.
func mergeContext(parent, values context.Context) context.Context {
	return &mergedCtx{
		Context: parent,
		values:  values,
	}
}

// mergedCtx delegates Done/Err/Deadline to the embedded (parent) Context
// and Value lookups to the values context.
type mergedCtx struct {
	context.Context
	values context.Context
}

func (m *mergedCtx) Value(key any) any {
	// Try the values context first (has our injected deps)
	if v := m.values.Value(key); v != nil {
		return v
	}
	// Fall back to parent
	return m.Context.Value(key)
}

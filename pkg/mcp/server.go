// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/go-logr/logr"
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

const serverInstructions = `scafctl is a CLI tool for managing infrastructure solutions using CEL expressions, 
Go templates, and a provider-based architecture. This MCP server exposes tools 
for inspecting solutions, validating configurations, evaluating CEL expressions, 
browsing the solution catalog, previewing resolver outputs, and running functional tests.

Most tools are read-only and safe to call. The following tools execute solution code 
and may have side effects depending on the providers used (e.g., exec, http):
  - preview_resolvers: executes the resolver chain
  - preview_action: builds action graph from live resolver data (executes resolvers, but NOT actions)
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

Scaffolding a New Solution:
  Call scaffold_solution with a name, description, and optional features/providers to 
  generate a complete skeleton YAML with examples. This is the fastest way to start.

Expression Debugging:
  - validate_expression: syntax-check CEL expressions or Go templates without running them
  - evaluate_go_template: render a Go template with sample data and see referenced fields
  - evaluate_cel: evaluate a CEL expression with data context

Composition Workflow:
  For multi-file solutions, use the compose_solution prompt to guide splitting a solution 
  into reusable partial YAML files. The solution://{name}/graph resource shows the resolver 
  dependency graph for a composed (or standalone) solution.

Provider Schema Reference:
  When creating or editing solution YAML (actions, resolvers), ALWAYS call 
  get_provider_schema with the provider name to verify exact input field names,
  types, which fields are required, and what outputs are available.
  The provider://reference resource gives a compact overview of all providers.

CLI Usage Reference (use these exact flags when suggesting commands to users):
  Run a solution:       scafctl run solution -f ./solution.yaml -r key=value
  Run resolvers only:   scafctl run resolver -f ./solution.yaml -r key=value
  Lint a solution:      scafctl lint -f ./solution.yaml
  Inspect a solution:   scafctl explain -f ./solution.yaml

IMPORTANT — choosing between 'run solution' and 'run resolver':
  • 'scafctl run solution' REQUIRES the solution to have a spec.workflow section with actions.
    It will FAIL with an error if the solution has no workflow defined.
  • 'scafctl run resolver' runs ONLY the resolvers and does NOT require a workflow.
    Use this when the solution has resolvers but no spec.workflow/actions section.
  • Rule of thumb: if the solution YAML contains spec.workflow.actions → use 'run solution'.
    If it does NOT have spec.workflow → use 'run resolver'.

IMPORTANT: Resolver parameters are passed with -r/--resolver, NOT -p. There is no -p flag.
Examples:
  scafctl run solution -f ./my-solution.yaml -r env=prod -r region=us-east1
  scafctl run solution my-catalog-solution -r inputText="Hello World" -r operation=uppercase
  scafctl run resolver -f ./my-solution.yaml -r name=value`

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
		server.WithToolCapabilities(false),
		server.WithResourceCapabilities(true, false),
		server.WithPromptCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions(serverInstructions),
	)

	// Register all tools
	s.registerTools()

	// Register all resources
	s.registerResources()

	// Register all prompts
	s.registerAllPrompts()

	return s, nil
}

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

	// Scaffold tools
	s.registerScaffoldTools()

	// Action preview tools
	s.registerActionTools()

	// Diff tools
	s.registerDiffTools()
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

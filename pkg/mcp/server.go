// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/mcp/upstream"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// configReloaderTTL is how long the MCP server caches config before
// re-reading from disk. Keeps long-lived sessions responsive to config
// changes (e.g. auth profile switches) without hitting disk every request.
const configReloaderTTL = 2 * time.Second

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
	rootCmd   *cobra.Command

	// prompts tracks registered prompts for listing. The mcp-go SDK
	// does not expose a public ListPrompts method, so we maintain our
	// own slice during registration.
	prompts []mcp.Prompt

	// coreTools tracks tool names registered by scafctl itself.
	// Tools not in this set are tagged as plugin/embedder tools.
	coreTools map[string]struct{}

	// corePrompts tracks prompt names registered by scafctl itself.
	// Prompts not in this set are tagged as plugin/embedder prompts.
	corePrompts map[string]struct{}

	// pluginPool manages shared, long-lived plugin processes with lazy
	// initialization and idle eviction. When set, provider tool handlers
	// auto-resolve official plugins on demand.
	pluginPool *plugin.Pool

	// cfgReloader provides TTL-based config reloading so long-lived MCP
	// sessions pick up config changes (e.g. auth profile switches).
	cfgReloader *configReloader

	// descriptorCache persists provider descriptors to disk so schemas
	// are available without spawning plugin processes.
	descriptorCache *plugin.DescriptorCache

	// upstreamProxies tracks upstream MCP server proxies keyed by server name.
	upstreamProxies map[string]*upstream.Proxy

	// upstreamTools tracks tool names that are proxied from upstream servers.
	// Maps tool name -> upstream server name.
	upstreamTools map[string]string

	// upstreamMu guards upstream registration state. Unlike sync.Once,
	// this allows retrying after transient failures.
	upstreamMu         sync.Mutex
	upstreamRegistered bool

	// sseServer is the SSE transport server (nil for stdio).
	sseServer *server.SSEServer
	// httpServer is the Streamable HTTP transport server (nil for stdio).
	httpServer *server.StreamableHTTPServer

	// profileOverride holds an optional auth profile name set via the
	// auth_set_profile MCP tool. When non-empty, freshConfigContext injects
	// it into every request context so all tool calls use that profile.
	// Stored as *string so atomic.Value can distinguish "never set" (nil)
	// from "set to a named profile" (non-empty *string). The code never
	// stores an empty string — clearing is done by storing nil.
	profileOverride atomic.Value // *string

	// subscriptions tracks MCP resource subscriptions and delivers live
	// resources/updated notifications when a watched solution file changes.
	subscriptions *subscriptionManager
}

// ServerOption configures the MCP server.
type ServerOption func(*serverConfig)

type serverConfig struct {
	logger                   *logr.Logger
	registry                 *provider.Registry
	authReg                  *auth.Registry
	config                   *config.Config
	version                  string
	name                     string
	ctx                      context.Context
	paginationLimit          int
	workerPoolSize           int
	queueSize                int
	errorLogger              *log.Logger
	supplementalInstructions string
	rootCmd                  *cobra.Command
	pluginPool               *plugin.Pool
	upstreamServers          map[string]config.MCPServerConfig
}

// WithRootCommand sets the cobra root command for CLI introspection tools.
func WithRootCommand(cmd *cobra.Command) ServerOption {
	return func(c *serverConfig) {
		c.rootCmd = cmd
	}
}

// WithServerPluginPool sets the plugin pool for auto-resolving official
// plugin providers on demand. When set, provider tool handlers (run_provider,
// get_provider_schema, get_provider_output_shape) will attempt to fetch and
// load official plugins that are not yet in the provider registry.
func WithServerPluginPool(pool *plugin.Pool) ServerOption {
	return func(c *serverConfig) {
		c.pluginPool = pool
	}
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

// WithSupplementalInstructions appends additional instruction text to the
// server instructions returned to AI agents during initialization.
// Embedders use this to provide routing guidance for their custom tools.
//
// The supplemental text is appended after the base server instructions,
// separated by a blank line. Binary-name substitution is automatically
// applied when the server name differs from the default.
//
// Example:
//
//	srv, err := mcp.NewServer(
//		mcp.WithServerName("mycli"),
//		mcp.WithSupplementalInstructions(`
//	This server includes domain-specific migration tools.
//	Use migration tools ONLY when working with legacy solution files.
//	For new solutions, use only the core tools above.
//	`),
//	)
func WithSupplementalInstructions(instructions string) ServerOption {
	return func(c *serverConfig) {
		c.supplementalInstructions = instructions
	}
}

// WithUpstreamServer adds an upstream MCP server whose tools are
// auto-discovered and proxied through the local server. Auth tokens are
// injected automatically using the configured auth handler.
//
// Multiple upstream servers can be added by calling this option multiple times.
func WithUpstreamServer(name string, cfg config.MCPServerConfig) ServerOption {
	return func(c *serverConfig) {
		if c.upstreamServers == nil {
			c.upstreamServers = make(map[string]config.MCPServerConfig)
		}
		c.upstreamServers[name] = cfg
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
  4. Call preview_resolvers to verify resolver outputs (use resolver param to focus on one)
  5. Call preview_action to dry-run the action graph and see materialized inputs
  6. Call run_solution_tests to run functional tests (use verbose=true for full assertion details)
  7. Call diff_solution to compare solution versions before committing changes
  8. Call get_run_command to get the exact CLI command for the user
  For lint findings, call explain_lint_rule with the ruleName, or use the fix_lint prompt.
  When the user wants to run a solution, use the prepare_execution prompt — it validates,
  previews, and generates the CLI command WITHOUT executing.

Reference tools (call these on demand instead of relying on this text):
  - list_context_variables: injected CEL/template context variables per phase (_, __plan,
    __execution, __actions, etc.)
  - list_cel_functions / list_go_template_functions: full function catalogs
  - get_provider_schema: exact input/output fields for a provider (ALWAYS check before
    writing actions/resolvers that use it); provider://reference gives a compact overview
  - get_run_command: exact CLI invocation, including --on-conflict/--backup/--show-execution
    flags and structured (json/yaml) failure-output shape
  - explain_concepts: narrative guidance (context-variables, phase-execution, cel-cost-model,
    template-dependency-inference, snapshot-masking, authoring-workflow)
  - extract_resolver_refs: find _.resolverName references in a tmpl/expr to populate dependsOn
  - generate_test_scaffold / list_tests: starter functional tests and test discovery
  - show_snapshot / diff_snapshots / analyze_execution prompt: post-execution debugging
  - compose_solution prompt / solution://{name}/graph resource: splitting multi-file solutions

IMPORTANT gotchas (not obvious from tool names, keep these in mind):
  • The test command is 'scafctl test functional -f <file>', NOT 'scafctl test -f <file>' —
    'functional' is a required subcommand and the -f flag belongs to it.
  • 'scafctl run solution' REQUIRES spec.workflow.actions and fails without it; use
    'scafctl run resolver' for solutions with resolvers but no workflow.
  • Resolver parameters use -r/--resolver or positional key=value — there is NO -p flag.
    Values can come from files (@file.yaml), stdin (@-), or raw content (key=@file).
  • When mentioning solution filenames in responses, always use a "./" prefix
    (e.g., "./my-solution.yaml") — bare filenames get auto-linkified into broken URLs by
    some clients.
  • The resolver "type" field is OPTIONAL and should usually be omitted. Only set it for
    known scalars; NEVER set type: string on a resolver whose provider returns an
    object/map (e.g., http returns {statusCode, body, headers}) — it causes coercion errors.`

// serverInstructions returns the MCP server instructions with the binary name
// substituted for all "scafctl" references.
func serverInstructions(name string) string {
	if name == settings.CliBinaryName {
		return serverInstructionsTemplate
	}
	return strings.ReplaceAll(serverInstructionsTemplate, settings.CliBinaryName, name)
}

// buildInstructions combines the base server instructions with optional
// supplemental instructions from embedders. Binary-name substitution is
// applied to supplemental text when the server name differs from the default.
func buildInstructions(name, supplemental string) string {
	base := serverInstructions(name)
	if supplemental == "" {
		return base
	}
	if name != settings.CliBinaryName {
		supplemental = strings.ReplaceAll(supplemental, settings.CliBinaryName, name)
	}
	return base + "\n\n" + supplemental
}

// resolveConfig returns the effective configuration. When a config reloader
// is present (normal operation), it delegates to the reloader which applies
// TTL-based caching and re-reads from disk as needed. This ensures long-lived
// MCP sessions pick up config changes (e.g. auth profile switches).
//
// Fallback order when no reloader is set (e.g. tests):
//  1. s.config (set during NewServer via WithServerConfig)
//  2. config.FromContext(s.ctx) (set during PersistentPreRun)
//  3. config.Global() (loads from disk)
func (s *Server) resolveConfig() *config.Config {
	if s.cfgReloader != nil {
		return s.cfgReloader.Config()
	}
	if s.config != nil {
		return s.config
	}
	if s.ctx != nil {
		if cfg := config.FromContext(s.ctx); cfg != nil {
			return cfg
		}
	}
	cfg, err := config.Global()
	if err != nil {
		s.logger.V(1).Info("failed to load global config", "error", err)
		return nil
	}
	return cfg
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

	// Validate that a registry is set when a plugin pool is configured,
	// otherwise ensureProvider would panic on nil registry access.
	if cfg.pluginPool != nil && cfg.registry == nil {
		return nil, errors.New("WithServerPluginPool requires WithServerRegistry")
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
		ctx:             mcpCtx,
		version:         cfg.version,
		name:            cfg.name,
		registry:        cfg.registry,
		authReg:         cfg.authReg,
		config:          cfg.config,
		rootCmd:         cfg.rootCmd,
		pluginPool:      cfg.pluginPool,
		coreTools:       make(map[string]struct{}, 64),
		corePrompts:     make(map[string]struct{}, 16),
		upstreamTools:   make(map[string]string),
		upstreamProxies: make(map[string]*upstream.Proxy),
	}

	// Ensure the official provider registry is available in the server
	// context so ensureProvider can resolve official plugin names --
	// but only when official providers are not explicitly disabled.
	// Check both the server config field and the config from context
	// (embedders may supply config via WithServerContext).
	officialDisabled := s.config != nil && s.config.Settings.DisableOfficialProviders
	if !officialDisabled {
		if ctxCfg := config.FromContext(s.ctx); ctxCfg != nil {
			officialDisabled = ctxCfg.Settings.DisableOfficialProviders
		}
	}
	if !officialDisabled && official.RegistryFromContext(s.ctx) == nil {
		s.ctx = official.WithRegistry(s.ctx, official.NewRegistry())
	}

	// Always initialize a descriptor cache for offline schema access.
	s.descriptorCache = plugin.NewDescriptorCache("", 24*time.Hour)
	if cfg.logger != nil {
		s.logger = *cfg.logger
	} else {
		s.logger = logr.Discard()
	}

	// Initialize the config reloader so long-lived MCP sessions pick up
	// config changes from disk (e.g. auth profile switches via CLI).
	// Seed with the best available initial config: explicit → context → nil.
	initialCfg := cfg.config
	if initialCfg == nil {
		initialCfg = config.FromContext(s.ctx)
	}
	s.cfgReloader = newConfigReloader(initialCfg, configReloaderTTL, s.logger)

	// Build observability hooks and add subscription lifecycle hooks so the
	// server can deliver live resources/updated notifications. The hooks
	// close over s, so they observe s.subscriptions once it is initialized
	// below (after the mcp-go server exists).
	obsHooks := newObservabilityHooks(s.logger)
	s.registerSubscriptionHooks(obsHooks)

	// Build server options for mcp-go
	serverOpts := []server.ServerOption{
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithPromptCapabilities(true),
		server.WithRecovery(),
		server.WithResourceRecovery(),
		server.WithInstructions(buildInstructions(cfg.name, cfg.supplementalInstructions)),
		// Enable advanced protocol capabilities
		server.WithLogging(),
		server.WithRoots(),
		server.WithElicitation(),
		server.WithCompletions(),
		// Task support for async long-running operations
		server.WithTaskCapabilities(true, true, true),
		// Observability hooks & middleware
		server.WithHooks(obsHooks),
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

	// Initialize the resource subscription manager now that the mcp-go server
	// (the notifier) exists. The subscription hooks registered above reference
	// s.subscriptions lazily, so they pick this up on the first subscribe.
	s.subscriptions = newSubscriptionManager(s.mcpServer, s.resolveSubscriptionFile, subscriptionDebounceDuration, s.logger)

	// Register all tools
	s.registerTools()

	// Install tool handler middleware that refreshes the context on every
	// tool call.  The stdio transport calls its contextFunc once at startup
	// (not per-request), so session-level state changes (e.g. profile
	// override from auth_set_profile) would otherwise be invisible to
	// subsequent tool handlers.  This middleware is transport-agnostic and
	// harmless for SSE/HTTP where the transport already refreshes per-request.
	s.mcpServer.Use(func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return next(s.freshConfigContext(ctx), req)
		}
	})

	// Register all resources
	s.registerResources()

	// Register all prompts
	s.registerAllPrompts()

	// Merge upstream servers from config and explicit options.
	// Check both WithServerConfig and config from context (WithServerContext)
	// so embedders who supply config via either path get upstream proxies.
	upstreamConfigs := make(map[string]config.MCPServerConfig)
	if cfg.config != nil {
		for name, serverCfg := range cfg.config.MCP.Servers {
			upstreamConfigs[name] = serverCfg
		}
	} else if ctxCfg := config.FromContext(mcpCtx); ctxCfg != nil {
		for name, serverCfg := range ctxCfg.MCP.Servers {
			upstreamConfigs[name] = serverCfg
		}
	}
	// Explicit WithUpstreamServer options override config entries.
	for name, serverCfg := range cfg.upstreamServers {
		upstreamConfigs[name] = serverCfg
	}

	// Create upstream proxies (actual connection is lazy).
	for name, serverCfg := range upstreamConfigs {
		if !serverCfg.IsEnabled() {
			s.logger.V(1).Info("upstream server disabled, skipping", "upstream", name)
			continue
		}
		if serverCfg.URL == "" {
			s.logger.Error(nil, "upstream server has no URL, skipping", "upstream", name)
			continue
		}
		proxy := upstream.NewProxy(name, serverCfg, s.authReg, s.logger, s.name)
		s.upstreamProxies[name] = proxy
	}

	return s, nil
}

// Close releases resources owned by the server. Safe to call multiple times
// and concurrently with Serve*/Handler/Info.
func (s *Server) Close() {
	if s.subscriptions != nil {
		s.subscriptions.Close()
	}

	s.upstreamMu.Lock()
	proxies := s.upstreamProxies
	s.upstreamProxies = make(map[string]*upstream.Proxy)
	s.upstreamRegistered = false
	s.upstreamMu.Unlock()

	for name, proxy := range proxies {
		if err := proxy.Close(); err != nil {
			s.logger.Error(err, "failed to close upstream proxy", "upstream", name)
		}
	}
}

// registerUpstreamTools connects to each upstream proxy, discovers its tools,
// and registers proxy handlers on the local server. Safe to call from
// multiple Serve*/Handler/Info methods. On full success the registration is
// sealed; on partial or total failure it can be retried on the next call so
// that transient startup/auth errors are not permanent.
func (s *Server) registerUpstreamTools(ctx context.Context) error {
	s.upstreamMu.Lock()
	defer s.upstreamMu.Unlock()

	if s.upstreamRegistered {
		return nil
	}

	err := s.doRegisterUpstreamTools(ctx)
	if err == nil {
		s.upstreamRegistered = true
	}
	return err
}

// doRegisterUpstreamTools performs upstream tool discovery and registration.
// Called via registerUpstreamTools with mutex guard; retryable on failure.
func (s *Server) doRegisterUpstreamTools(ctx context.Context) error {
	if len(s.upstreamProxies) == 0 {
		return nil
	}

	// Sort upstream names for deterministic registration order.
	names := make([]string, 0, len(s.upstreamProxies))
	for name := range s.upstreamProxies {
		names = append(names, name)
	}
	sort.Strings(names)

	// Build a set of all currently registered tool names so we can detect
	// collisions with core, embedder/plugin, and other upstream tools.
	registeredTools := make(map[string]struct{}, len(s.coreTools))
	for _, st := range s.mcpServer.ListTools() {
		registeredTools[st.Tool.Name] = struct{}{}
	}

	var errs []error
	for _, name := range names {
		proxy := s.upstreamProxies[name]
		tools, err := proxy.ListTools(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("upstream %q: %w", name, err))
			continue
		}

		for _, tool := range tools {
			toolName := tool.Name

			// Detect collisions with any already-registered tool (core,
			// embedder/plugin, or previously registered upstream).
			if _, exists := registeredTools[toolName]; exists {
				if existingUpstream, isDup := s.upstreamTools[toolName]; isDup && existingUpstream == name {
					// Already registered by this upstream in a prior attempt -- skip silently.
					continue
				} else if _, isCoreConflict := s.coreTools[toolName]; isCoreConflict {
					s.logger.Error(nil, "upstream tool conflicts with core tool, skipping",
						"tool", toolName, "upstream", name)
				} else if isDup {
					s.logger.Error(nil, "upstream tool name collision, skipping duplicate",
						"tool", toolName, "upstream", name, "existing_upstream", existingUpstream)
				} else {
					s.logger.Error(nil, "upstream tool conflicts with existing tool, skipping",
						"tool", toolName, "upstream", name)
				}
				continue
			}

			// Capture proxy for the closure.
			p := proxy
			s.mcpServer.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return p.CallTool(ctx, request.Params.Name, request.GetArguments())
			})
			s.upstreamTools[toolName] = name
			registeredTools[toolName] = struct{}{}
			s.logger.V(1).Info("registered upstream tool", "tool", toolName, "upstream", name)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// freshConfigContext returns a context with the latest config overlaid.
// Called per tool call via the Use() middleware registered in NewServer, and
// additionally by each transport's context func.  The middleware ensures that
// even transports that call their context func only once (stdio) still see
// up-to-date configuration on every tool invocation (e.g. after an auth
// profile switch via auth_set_profile).
func (s *Server) freshConfigContext(ctx context.Context) context.Context {
	merged := mergeContext(ctx, s.ctx)
	if s.cfgReloader != nil {
		if cfg := s.cfgReloader.Config(); cfg != nil {
			merged = config.WithConfig(merged, cfg)
		}
	}
	// Apply session-level profile override set by auth_set_profile tool.
	if p, ok := s.profileOverride.Load().(*string); ok && p != nil && *p != "" {
		merged = auth.WithProfile(merged, *p)
	}
	return merged
}

// Serve starts the MCP server on stdio transport (blocking).
// Server context values (auth registry, config, settings, logger) are
// automatically injected into the transport's request context so that
// all tool handlers -- including those registered by embedders via
// MCPServer().AddTool() -- can access them.
func (s *Server) Serve(opts ...server.StdioOption) error {
	// Discover and register upstream tools before serving.
	if err := s.registerUpstreamTools(s.ctx); err != nil {
		s.logger.Error(err, "failed to register upstream tools")
	}

	contextOpt := server.WithStdioContextFunc(func(ctx context.Context) context.Context {
		return s.freshConfigContext(ctx)
	})
	allOpts := make([]server.StdioOption, len(opts)+1)
	copy(allOpts, opts)
	allOpts[len(opts)] = contextOpt
	return server.ServeStdio(s.mcpServer, allOpts...)
}

// ServeSSE starts the MCP server on SSE transport at the given address.
// Server context values are injected into every request context (see Serve).
func (s *Server) ServeSSE(addr string, opts ...server.SSEOption) error {
	// Discover and register upstream tools before serving.
	if err := s.registerUpstreamTools(s.ctx); err != nil {
		s.logger.Error(err, "failed to register upstream tools")
	}

	contextOpt := server.WithSSEContextFunc(func(ctx context.Context, _ *http.Request) context.Context {
		return s.freshConfigContext(ctx)
	})
	allOpts := make([]server.SSEOption, len(opts)+1)
	copy(allOpts, opts)
	allOpts[len(opts)] = contextOpt
	s.sseServer = server.NewSSEServer(s.mcpServer, allOpts...)
	s.logger.Info("starting SSE server", "addr", addr)
	return s.sseServer.Start(addr)
}

// ServeHTTP starts the MCP server on Streamable HTTP transport at the given address.
// Server context values are injected into every request context (see Serve).
func (s *Server) ServeHTTP(addr string, opts ...server.StreamableHTTPOption) error {
	// Discover and register upstream tools before serving.
	if err := s.registerUpstreamTools(s.ctx); err != nil {
		s.logger.Error(err, "failed to register upstream tools")
	}

	contextOpt := server.WithHTTPContextFunc(func(ctx context.Context, _ *http.Request) context.Context {
		return s.freshConfigContext(ctx)
	})
	allOpts := make([]server.StreamableHTTPOption, len(opts)+1)
	copy(allOpts, opts)
	allOpts[len(opts)] = contextOpt
	s.httpServer = server.NewStreamableHTTPServer(s.mcpServer, allOpts...)
	s.logger.Info("starting Streamable HTTP server", "addr", addr)
	return s.httpServer.Start(addr)
}

// Handler returns an http.Handler for the Streamable HTTP transport.
// Server context values are injected into every request context (see Serve).
// If the HTTP server was already created (by a prior call to Handler or ServeHTTP),
// the existing instance is returned and opts are ignored.
func (s *Server) Handler(opts ...server.StreamableHTTPOption) http.Handler {
	// Discover and register upstream tools before returning the handler.
	if err := s.registerUpstreamTools(s.ctx); err != nil {
		s.logger.Error(err, "failed to register upstream tools")
	}

	if s.httpServer != nil {
		return s.httpServer
	}
	contextOpt := server.WithHTTPContextFunc(func(ctx context.Context, _ *http.Request) context.Context {
		return s.freshConfigContext(ctx)
	})
	allOpts := make([]server.StreamableHTTPOption, len(opts)+1)
	copy(allOpts, opts)
	allOpts[len(opts)] = contextOpt
	s.httpServer = server.NewStreamableHTTPServer(s.mcpServer, allOpts...)
	return s.httpServer
}

// MCPServer returns the underlying mcp-go MCPServer.
// This is useful for advanced operations like sending notifications.
//
// Embedders can call MCPServer().AddTool() to register custom tools;
// these are automatically listed by ListCapabilities with SourcePlugin.
//
// Note: prompts added via MCPServer().AddPrompt() will NOT appear in
// ListCapabilities because the mcp-go SDK does not expose a ListPrompts
// method. Use [Server.AddPrompt] instead to register prompts that should
// be discoverable.
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
	// Ensure upstream tools are registered so info output matches runtime.
	if err := s.registerUpstreamTools(s.ctx); err != nil {
		s.logger.Error(err, "failed to register upstream tools for info")
	}
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

	// Extract tool info from registered tools, applying the contextual
	// filter so the output matches what MCP clients actually discover.
	registered := s.mcpServer.ListTools()
	allTools := make([]mcp.Tool, 0, len(registered))
	for _, st := range registered {
		allTools = append(allTools, st.Tool)
	}

	filterFn := contextualToolFilter(s)
	visible := filterFn(context.Background(), allTools)

	sort.Slice(visible, func(i, j int) bool {
		return visible[i].Name < visible[j].Name
	})

	for _, tool := range visible {
		info.Tools = append(info.Tools, toolInfo{
			Name:        tool.Name,
			Description: tool.Description,
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
	s.registerCatalogSearchTools()

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

	// Run tools (execute solutions via domain layer)
	s.registerRunTools()

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

	// Plugin cache tools (list, cache path)
	s.registerPluginTools()

	// Version tool
	s.registerVersionTools()

	// REST API tools
	s.registerAPITools()

	// State inspection tools
	s.registerStateTools()

	// CLI introspection tools
	s.registerCLITools()
}

// registerResources registers all MCP resources on the server.
func (s *Server) registerResources() {
	s.registerResourceTemplates()
}

// addTool registers a tool with the MCP server and tracks it as a core
// tool for listing via ListCapabilities. Embedders that call
// MCPServer().AddTool() directly will have their tools listed as plugin.
func (s *Server) addTool(tool mcp.Tool, handler server.ToolHandlerFunc) {
	s.mcpServer.AddTool(tool, handler)
	s.coreTools[tool.Name] = struct{}{}
}

// AddPrompt registers a prompt with the MCP server and tracks it for
// listing via ListCapabilities. Embedders should use this instead of
// MCPServer().AddPrompt() to ensure the prompt appears in listings.
func (s *Server) AddPrompt(prompt mcp.Prompt, handler server.PromptHandlerFunc) {
	s.mcpServer.AddPrompt(prompt, handler)
	s.prompts = append(s.prompts, prompt)
}

// addCorePrompt registers a prompt and marks it as a core prompt.
func (s *Server) addCorePrompt(prompt mcp.Prompt, handler server.PromptHandlerFunc) {
	s.AddPrompt(prompt, handler)
	s.corePrompts[prompt.Name] = struct{}{}
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

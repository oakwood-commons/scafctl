// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package api provides a chi+Huma REST API server for scafctl.
// It mirrors all major CLI features with authentication, metrics,
// tracing, and audit logging.
package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	catalogindex "github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/runmode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// Server is the REST API server backed by chi and Huma.
type Server struct {
	cfg               *config.Config
	router            *chi.Mux
	apiRouter         chi.Router
	api               huma.API
	httpSrv           *http.Server
	isShuttingDown    int32
	startTime         time.Time
	logger            logr.Logger
	providerReg       *provider.Registry
	compositeReg      *provider.CompositeRegistry
	authReg           *auth.Registry
	pluginFetcher     *plugin.Fetcher
	officialProviders *official.Registry
	pluginClients     []*plugin.Client
	pluginPool        PluginPool
	version           string
	ctx               context.Context
	cancel            context.CancelFunc

	// catalogIndex is built once at server start from the configured catalogs
	// and shared with every HandlerContext.
	catalogIndex *catalogindex.Index
}

// serverConfig collects all configuration options before building the server.
type serverConfig struct {
	logger            *logr.Logger
	registry          *provider.Registry
	compositeReg      *provider.CompositeRegistry
	authReg           *auth.Registry
	pluginFetcher     *plugin.Fetcher
	officialProviders *official.Registry
	pluginClients     []*plugin.Client
	pluginPool        PluginPool
	config            *config.Config
	version           string
	ctx               context.Context

	catalogIndex *catalogindex.Index
}

// ServerOption configures the API server.
type ServerOption func(*serverConfig)

// WithServerLogger sets the logger for the API server.
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

// WithServerCompositeRegistry sets the composite (builtin + external) provider
// registry used by solution endpoints to build a request-scoped registry via
// prepare.ScopeSolutionProviders. It is separate from WithServerRegistry, which
// wires the legacy *provider.Registry surface still consumed by other handlers.
func WithServerCompositeRegistry(reg *provider.CompositeRegistry) ServerOption {
	return func(c *serverConfig) {
		c.compositeReg = reg
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

// WithServerPluginFetcher sets the plugin fetcher for auto-fetching plugin
// binaries from catalogs. This enables solution endpoints to resolve
// bundle.plugins declarations and official provider auto-resolution.
func WithServerPluginFetcher(f *plugin.Fetcher) ServerOption {
	return func(c *serverConfig) {
		c.pluginFetcher = f
	}
}

// WithServerOfficialProviders sets the official provider registry for
// auto-resolution of first-party extracted providers.
func WithServerOfficialProviders(r *official.Registry) ServerOption {
	return func(c *serverConfig) {
		c.officialProviders = r
	}
}

// WithServerPluginClients sets the pre-loaded plugin clients that should be
// killed when the server shuts down.
func WithServerPluginClients(clients []*plugin.Client) ServerOption {
	return func(c *serverConfig) {
		c.pluginClients = clients
	}
}

// WithServerPluginPool sets the plugin pool for managing shared, long-lived
// plugin processes with lazy initialization and idle eviction. Any type
// satisfying PluginPool (e.g. *plugin.Pool or *plugin.VersionPool) is accepted.
func WithServerPluginPool(pool PluginPool) ServerOption {
	return func(c *serverConfig) {
		c.pluginPool = pool
	}
}

// WithServerCatalogIndex sets the config-derived catalog topology shared with
// every HandlerContext. It is built once at server start from the configured
// catalogs (see catalogindex.FromConfig).
func WithServerCatalogIndex(idx *catalogindex.Index) ServerOption {
	return func(c *serverConfig) {
		c.catalogIndex = idx
	}
}

// NewServer creates a new API server with the given options.
func NewServer(opts ...ServerOption) (*Server, error) {
	sc := &serverConfig{}
	for _, o := range opts {
		o(sc)
	}

	lgr := logr.Discard()
	if sc.logger != nil {
		lgr = *sc.logger
	}

	cfg := sc.config
	if cfg == nil {
		cfg = &config.Config{}
	}

	// Validate the API section here, not just in the config loader.
	// config.Manager.Load is the only other caller of Validate, so an embedder
	// passing a hand-built config through WithServerConfig would otherwise skip
	// every advertised apiServer bound -- including maxHeaderBytes, which sizes
	// a per-connection buffer before any request handling. Failing at
	// construction keeps the documented limits true for every path that can
	// start a server.
	if err := cfg.APIServer.Validate(); err != nil {
		return nil, fmt.Errorf("invalid apiServer configuration: %w", err)
	}

	baseCtx := sc.ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(baseCtx)

	version := sc.version
	if version == "" {
		version = settings.VersionInformation.BuildVersion
	}

	s := &Server{
		cfg:               cfg,
		router:            chi.NewRouter(),
		logger:            lgr,
		providerReg:       sc.registry,
		compositeReg:      sc.compositeReg,
		authReg:           sc.authReg,
		pluginFetcher:     sc.pluginFetcher,
		officialProviders: sc.officialProviders,
		pluginClients:     sc.pluginClients,
		pluginPool:        sc.pluginPool,
		version:           version,
		ctx:               ctx,
		cancel:            cancel,
		startTime:         time.Now(),

		catalogIndex: sc.catalogIndex,
	}

	// Attach the application config to every request context, matching what the
	// CLI (cmd/scafctl/root.go) and MCP server (pkg/mcp/context.go) already do.
	//
	// Without this, config.FromContext returns nil inside API handlers, so
	// config-driven behaviour -- including the httpClient.allowPrivateIPs SSRF
	// setting consulted when fetching a solution by URL -- silently fell back to
	// defaults and could not be configured by an operator at all.
	s.router.Use(withAppConfig(cfg))

	return s, nil
}

// withAppConfig returns middleware that places cfg in each request's context.
func withAppConfig(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(config.WithConfig(r.Context(), cfg)))
		})
	}
}

// Router returns the root chi router for global middleware setup.
func (s *Server) Router() *chi.Mux {
	return s.router
}

// SetAPIRouter sets the API route group (returned by SetupMiddleware).
func (s *Server) SetAPIRouter(r chi.Router) {
	s.apiRouter = r
}

// APIRouter returns the API route group. Falls back to the root router if unset.
func (s *Server) APIRouter() chi.Router {
	if s.apiRouter != nil {
		return s.apiRouter
	}
	return s.router
}

// API returns the Huma API instance for endpoint registration.
// Panics if called before InitAPI().
func (s *Server) API() huma.API {
	if s.api == nil {
		panic("api.Server.API() called before InitAPI(): call InitAPI() after SetupMiddleware()")
	}
	return s.api
}

// Config returns the server's application config.
func (s *Server) Config() *config.Config {
	return s.cfg
}

// Version returns the server version string.
func (s *Server) Version() string {
	return s.version
}

// IsShuttingDown returns true if the server is in graceful shutdown.
func (s *Server) IsShuttingDown() bool {
	return atomic.LoadInt32(&s.isShuttingDown) == 1
}

// StartTime returns when the server started.
func (s *Server) StartTime() time.Time {
	return s.startTime
}

// Context returns the server's cancellable context. It is cancelled when the
// server shuts down (Shutdown is called or Start exits). Middleware that
// launches background goroutines should use this context rather than the outer
// cobra command context so goroutines are stopped as part of server shutdown.
func (s *Server) Context() context.Context {
	return s.ctx
}

// HandlerCtx returns the handler context for endpoint registration.
func (s *Server) HandlerCtx() *HandlerContext {
	hctx := NewHandlerContext(
		s.cfg,
		s.providerReg,
		s.authReg,
		s.logger,
		&s.isShuttingDown,
		s.startTime,
	)
	hctx.PluginFetcher = s.pluginFetcher
	hctx.OfficialProviders = s.officialProviders
	hctx.ServerContext = s.ctx
	hctx.PluginPool = s.pluginPool
	hctx.CompositeRegistry = s.compositeReg
	hctx.CatalogIndex = s.catalogIndex
	return hctx
}

// Start starts the HTTP server and blocks until shutdown.
func (s *Server) Start() error {
	// Cancel the server context when Start returns so background goroutines
	// (e.g. rate-limit cleanup) are stopped on all exit paths, including
	// early TLS validation failures.
	defer s.cancel()

	apiCfg := s.cfg.APIServer

	addr := s.buildHTTPServer()

	// TLS configuration
	if apiCfg.TLS.Enabled {
		if apiCfg.TLS.Cert == "" || apiCfg.TLS.Key == "" {
			return fmt.Errorf("TLS enabled but cert or key path is empty")
		}
		cert, err := tls.LoadX509KeyPair(apiCfg.TLS.Cert, apiCfg.TLS.Key)
		if err != nil {
			return fmt.Errorf("loading TLS certificate: %w", err)
		}
		s.httpSrv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}

	// Graceful shutdown goroutine
	shutdownDone := make(chan error, 1)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		select {
		case sig := <-sigCh:
			s.logger.Info("received signal, starting graceful shutdown", "signal", sig.String())
		case <-s.ctx.Done():
			s.logger.Info("context cancelled, starting graceful shutdown")
		}

		atomic.StoreInt32(&s.isShuttingDown, 1)

		timeout := parseTimeoutOrDefault(apiCfg.ShutdownTimeout, settings.DefaultAPIShutdownTimeout)
		shutCtx, shutCancel := context.WithTimeout(context.Background(), timeout)
		defer shutCancel()
		shutdownDone <- s.httpSrv.Shutdown(shutCtx)
	}()

	s.logger.Info("starting API server", "addr", addr, "tls", apiCfg.TLS.Enabled, "version", s.version)

	var err error
	if apiCfg.TLS.Enabled {
		// TLSConfig already set, pass empty cert/key paths
		err = s.httpSrv.ListenAndServeTLS("", "")
	} else {
		err = s.httpSrv.ListenAndServe()
	}

	if err == http.ErrServerClosed {
		// Wait for shutdown to complete
		return <-shutdownDone
	}
	return err
}

// Shutdown gracefully shuts down the server and kills plugin clients.
func (s *Server) Shutdown(ctx context.Context) error {
	atomic.StoreInt32(&s.isShuttingDown, 1)
	s.cancel()

	// Shut down plugin pool (kills all managed plugin processes)
	if s.pluginPool != nil {
		s.pluginPool.Shutdown()
	}

	// Kill pre-loaded plugin clients not managed by the pool
	for _, c := range s.pluginClients {
		c.Kill()
	}

	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}

// buildHTTPServer constructs s.httpSrv from configuration and returns the
// resolved listen address. It performs no I/O and binds no port, so the
// server's resource limits and exposure warning can be exercised in tests
// without starting a listener.
func (s *Server) buildHTTPServer() string {
	apiCfg := s.cfg.APIServer

	host := apiCfg.Host
	if host == "" {
		host = settings.DefaultAPIHost
	}
	port := apiCfg.Port
	if port <= 0 {
		port = settings.DefaultAPIPort
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	// Exposure warnings are NOT emitted here. They are returned by
	// StartupWarnings so the caller can put them on stderr, where a human sees
	// them regardless of logging level -- see that function for why a logger is
	// the wrong channel for them.

	maxHeaderBytes := apiCfg.MaxHeaderBytes
	if maxHeaderBytes <= 0 {
		maxHeaderBytes = settings.DefaultAPIMaxHeaderBytes
	}

	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       parseTimeoutOrDefault(apiCfg.RequestTimeout, settings.DefaultAPIRequestTimeout),
		WriteTimeout:      parseTimeoutOrDefault(apiCfg.RequestTimeout, settings.DefaultAPIRequestTimeout),
		IdleTimeout:       parseTimeoutOrDefault(apiCfg.IdleTimeout, settings.DefaultAPIIdleTimeout),
		MaxHeaderBytes:    maxHeaderBytes,
		BaseContext: func(_ net.Listener) context.Context {
			return runmode.WithMode(s.ctx, runmode.API)
		},
	}

	return addr
}

// parseTimeoutOrDefault parses a duration string, returning a default on failure.
func parseTimeoutOrDefault(value, defaultValue string) time.Duration {
	if value == "" {
		value = defaultValue
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		d, _ = time.ParseDuration(defaultValue)
	}
	return d
}

// isLoopbackHost reports whether host refers only to the local machine.
//
// It recognises loopback IP literals and the "localhost" name. An empty host
// is treated as loopback because the caller substitutes the loopback default
// before this is called. The wildcard addresses ("0.0.0.0", "::") are NOT
// loopback: they bind every interface and are the case worth warning about.
func isLoopbackHost(host string) bool {
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	// Tolerate a bracketed IPv6 literal (e.g. "[::1]").
	trimmed := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if ip := net.ParseIP(trimmed); ip != nil {
		return ip.IsLoopback()
	}
	// An unresolvable hostname is not provably local; treat it as exposed so
	// the warning errs toward being shown rather than silently skipped.
	return false
}

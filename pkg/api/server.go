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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// Server is the REST API server backed by chi and Huma.
type Server struct {
	cfg            *config.Config
	router         *chi.Mux
	apiRouter      chi.Router
	api            huma.API
	httpSrv        *http.Server
	isShuttingDown int32
	startTime      time.Time
	logger         logr.Logger
	providerReg    *provider.Registry
	authReg        *auth.Registry
	version        string
	ctx            context.Context
	cancel         context.CancelFunc
}

// serverConfig collects all configuration options before building the server.
type serverConfig struct {
	logger   *logr.Logger
	registry *provider.Registry
	authReg  *auth.Registry
	config   *config.Config
	version  string
	ctx      context.Context
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
		cfg:         cfg,
		router:      chi.NewRouter(),
		logger:      lgr,
		providerReg: sc.registry,
		authReg:     sc.authReg,
		version:     version,
		ctx:         ctx,
		cancel:      cancel,
		startTime:   time.Now(),
	}

	return s, nil
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
	return NewHandlerContext(
		s.cfg,
		s.providerReg,
		s.authReg,
		s.logger,
		&s.isShuttingDown,
		s.startTime,
	)
}

// Start starts the HTTP server and blocks until shutdown.
func (s *Server) Start() error {
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

	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       parseTimeoutOrDefault(apiCfg.RequestTimeout, settings.DefaultAPIRequestTimeout),
		WriteTimeout:      parseTimeoutOrDefault(apiCfg.RequestTimeout, settings.DefaultAPIRequestTimeout),
	}

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

	// Ensure context is cancelled when Start returns so the shutdown goroutine
	// exits even if ListenAndServe returns a non-ErrServerClosed error (e.g.
	// "address already in use") before a signal is received.
	defer s.cancel()

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

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	atomic.StoreInt32(&s.isShuttingDown, 1)
	s.cancel()
	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
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

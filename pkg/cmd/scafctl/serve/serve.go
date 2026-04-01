// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package serve

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/api/endpoints"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// Options holds the options for the serve command.
type Options struct {
	Host       string
	Port       int
	TLSCert    string
	TLSKey     string
	EnableTLS  bool
	APIVersion string
	CliParams  *settings.Run
	IOStreams  *terminal.IOStreams
}

// CommandServe creates the `scafctl serve` command.
func CommandServe(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &Options{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the REST API server",
		Long: heredoc.Doc(`
			Start the scafctl REST API server.

			The API server exposes all major scafctl features as REST endpoints:
			solutions, providers, catalogs, schemas, eval, config, and more.

			The server uses chi for routing and Huma for OpenAPI-compliant
			endpoint registration, with support for Entra OIDC authentication,
			Prometheus metrics, OpenTelemetry tracing, and audit logging.

			Health probes at /health, /health/live, and /health/ready bypass
			authentication for orchestrator integration.

			OpenAPI documentation is served at /{version}/docs and the spec
			at /{version}/openapi.
		`),
		Example: heredoc.Doc(`
			# Start the API server with defaults (port 8080)
			scafctl serve

			# Start on a custom port
			scafctl serve --port 9090

			# Start with TLS
			scafctl serve --enable-tls --tls-cert cert.pem --tls-key key.pem

			# Export OpenAPI spec without starting the server
			scafctl serve openapi --format yaml --output openapi.yaml
		`),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.Host, "host", "", "Host to bind to (default from config or 0.0.0.0)")
	cmd.Flags().IntVar(&opts.Port, "port", 0, "Port to listen on (default from config or 8080)")
	cmd.Flags().StringVar(&opts.TLSCert, "tls-cert", "", "Path to TLS certificate")
	cmd.Flags().StringVar(&opts.TLSKey, "tls-key", "", "Path to TLS private key")
	cmd.Flags().BoolVar(&opts.EnableTLS, "enable-tls", false, "Enable TLS")
	cmd.Flags().StringVar(&opts.APIVersion, "api-version", "", "API version prefix (default from config or v1)")

	cmd.AddCommand(CommandOpenAPI(cliParams, ioStreams))

	return cmd
}

func runServe(ctx context.Context, opts *Options) error {
	lgr := logger.FromContext(ctx)
	cfg := config.FromContext(ctx)
	authReg := auth.RegistryFromContext(ctx)

	// Apply CLI flag overrides to config
	if cfg == nil {
		cfg = &config.Config{}
	}
	if opts.Host != "" {
		cfg.APIServer.Host = opts.Host
	}
	if opts.Port > 0 {
		cfg.APIServer.Port = opts.Port
	}
	if opts.EnableTLS {
		cfg.APIServer.TLS.Enabled = true
	}
	if opts.TLSCert != "" {
		cfg.APIServer.TLS.Cert = opts.TLSCert
	}
	if opts.TLSKey != "" {
		cfg.APIServer.TLS.Key = opts.TLSKey
	}
	if opts.APIVersion != "" {
		cfg.APIServer.APIVersion = opts.APIVersion
	}

	// Load provider registry
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		return fmt.Errorf("initializing provider registry: %w", err)
	}

	// Build server options
	serverOpts := []api.ServerOption{
		api.WithServerLogger(*lgr),
		api.WithServerConfig(cfg),
		api.WithServerRegistry(reg),
		api.WithServerContext(ctx),
		api.WithServerVersion(settings.VersionInformation.BuildVersion),
	}
	if authReg != nil {
		serverOpts = append(serverOpts, api.WithServerAuthRegistry(authReg))
	}

	// Create server
	srv, err := api.NewServer(serverOpts...)
	if err != nil {
		return fmt.Errorf("creating API server: %w", err)
	}

	// Setup middleware (two-layer: global + API)
	// Use the server's own cancellable context so the rate-limit cleanup
	// goroutine is stopped when the server shuts down, not when the outer
	// cobra command context is eventually cancelled.
	apiRouter, err := api.SetupMiddleware(srv.Context(), srv.Router(), &cfg.APIServer, *lgr)
	if err != nil {
		return fmt.Errorf("setting up middleware: %w", err)
	}
	srv.SetAPIRouter(apiRouter)

	// Initialize Huma API
	srv.InitAPI()

	// Register all endpoints
	handlerCtx := srv.HandlerCtx()
	endpoints.RegisterAll(srv.API(), srv.Router(), handlerCtx)

	// Start server (blocks until SIGINT/SIGTERM)
	return srv.Start()
}

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"context"
	"os"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/entra"
	gcpauth "github.com/oakwood-commons/scafctl/pkg/auth/gcp"
	ghauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
	authcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/build"
	bundlecmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/bundle"
	cachecmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/cache"
	catalogcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/catalog"
	configcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/config"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/eval"
	examplescmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/examples"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/explain"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/lint"
	mcpcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/mcp"
	newcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/new"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/render"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/resolver"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/run"
	secretscmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/secrets"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/snapshot"
	testcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/test"
	vendorcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/vendor"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/version"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/profiler"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// RootOptions configures a Root() invocation. All fields are optional;
// nil values use production defaults. Passing a non-nil RootOptions
// enables fully parallel in-process execution because each call
// to Root() creates its own isolated state.
type RootOptions struct {
	// IOStreams overrides the default os.Stdin/Stdout/Stderr streams.
	// When nil, standard OS streams are used.
	IOStreams *terminal.IOStreams

	// ExitFunc overrides os.Exit. When non-nil it is passed through
	// writer.WithExitFunc so that ErrorWithCode/ErrorWithExit call
	// this function instead of terminating the process. Useful for
	// in-process test execution where panicking or returning an error
	// is preferred over killing the process.
	ExitFunc func(code int)

	// ConfigPath overrides the --config flag default.
	ConfigPath string
}

// NewRootOptions returns a RootOptions with production defaults
// (nil IOStreams, nil ExitFunc, empty ConfigPath).
func NewRootOptions() *RootOptions {
	return &RootOptions{}
}

// Root creates and returns the root cobra.Command for the scafctl CLI tool.
// It sets up persistent flags, configures logging, handles profiler options,
// validates command arguments, and adds subcommands. The command provides
// configuration discovery and scaffolding functionality.
//
// opts may be nil, in which case production defaults are used.
// Each invocation creates its own isolated state so multiple Root()
// calls can execute concurrently without data races.
func Root(opts *RootOptions) *cobra.Command {
	if opts == nil {
		opts = NewRootOptions()
	}

	// Per-invocation state — no package-level mutable variables.
	cliParams := settings.NewCliParams()
	var (
		configPath   = opts.ConfigPath
		debugFlag    bool
		logFormat    = "console"
		logFile      string
		otelInsecure bool
		telShutdown  func(context.Context) error
	)

	// Resolve IOStreams: use caller-provided or default to OS streams.
	ioStreams := opts.IOStreams
	if ioStreams == nil {
		ioStreams = terminal.NewIOStreams(os.Stdin, os.Stdout, os.Stderr, true)
	}

	// Build writer options (e.g. custom exit function for in-process test execution).
	var writerOpts []writer.Option
	if opts.ExitFunc != nil {
		writerOpts = append(writerOpts, writer.WithExitFunc(opts.ExitFunc))
	}

	cCmd := &cobra.Command{
		Use:   "scafctl",
		Short: "A configuration discovery and scaffolding tool",
		Long: heredoc.Doc(`
			A configuration discovery and scaffolding tool
		`),
		SilenceUsage:  false,
		SilenceErrors: true,
		PersistentPreRun: func(cCmd *cobra.Command, args []string) {
			// Load configuration first (before logger setup so config can influence log level)
			mgr := config.NewManager(configPath)
			cfg, err := mgr.Load()
			if err != nil {
				// Use stderr directly since writer isn't set up yet
				_, _ = ioStreams.ErrOut.Write([]byte("Warning: failed to load config: " + err.Error() + "\n"))
				// Continue with defaults
				cfg = &config.Config{}
			}

			// Apply config settings to cliParams (CLI flags take precedence)
			if !cCmd.Flags().Changed("no-color") {
				cliParams.NoColor = cfg.Settings.NoColor
			}
			if !cCmd.Flags().Changed("quiet") {
				cliParams.IsQuiet = cfg.Settings.Quiet
			}

			// Resolve log level with precedence: flag > --debug > env > config > default ("none")
			resolvedLogLevel := cliParams.MinLogLevel // flag value or default
			if !cCmd.Flags().Changed("log-level") {
				// Flag not explicitly set — check env vars, then config
				if envLevel := os.Getenv("SCAFCTL_LOG_LEVEL"); envLevel != "" {
					resolvedLogLevel = envLevel
				} else if envDebug := os.Getenv("SCAFCTL_DEBUG"); envDebug != "" && envDebug != "0" && envDebug != "false" {
					resolvedLogLevel = logger.LevelDebug
				} else if cfg.Logging.Level != "" {
					resolvedLogLevel = cfg.Logging.Level
				}
			}
			// --debug flag always wins (it's an explicit override)
			if debugFlag {
				resolvedLogLevel = logger.LevelDebug
			}
			cliParams.MinLogLevel = resolvedLogLevel

			// Resolve log format with precedence: flag > env > config > default ("console")
			resolvedFormat := logFormat // flag value or default
			if !cCmd.Flags().Changed("log-format") {
				if envFormat := os.Getenv("SCAFCTL_LOG_FORMAT"); envFormat != "" {
					resolvedFormat = envFormat
				} else if cfg.Logging.Format != "" {
					resolvedFormat = cfg.Logging.Format
				}
			}

			// Resolve log file with precedence: flag > env > default (empty = stderr)
			resolvedLogFile := logFile
			if !cCmd.Flags().Changed("log-file") {
				if envPath := os.Getenv("SCAFCTL_LOG_PATH"); envPath != "" {
					resolvedLogFile = envPath
				}
			}

			// ── OTel setup (must run before logger so otellogr picks up real provider) ──
			// Priority: CLI flag > OTEL_EXPORTER_OTLP_ENDPOINT env var > config file > default (empty)
			otelEndpoint := cfg.Telemetry.Endpoint
			if envEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); envEndpoint != "" {
				otelEndpoint = envEndpoint
			}
			if cCmd.Flags().Changed("otel-endpoint") {
				otelEndpoint, _ = cCmd.Flags().GetString("otel-endpoint")
			}
			// Priority for insecure: CLI flag > config file > default (false)
			resolvedOtelInsecure := cfg.Telemetry.Insecure
			if cCmd.Flags().Changed("otel-insecure") {
				resolvedOtelInsecure = otelInsecure
			}
			// Service name: CLI has no override flag; config file > default (binary name)
			serviceName := settings.CliBinaryName
			if cfg.Telemetry.ServiceName != "" {
				serviceName = cfg.Telemetry.ServiceName
			}
			telShutdown, err = telemetry.Setup(context.Background(), telemetry.Options{
				ServiceName:      serviceName,
				ServiceVersion:   settings.VersionInformation.BuildVersion,
				ExporterEndpoint: otelEndpoint,
				ExporterInsecure: resolvedOtelInsecure,
			})
			if err != nil {
				_, _ = ioStreams.ErrOut.Write([]byte("Warning: failed to initialize telemetry: " + err.Error() + "\n"))
			}

			// Initialise OTel metric instruments (must run after telemetry.Setup).
			if initErr := metrics.InitMetrics(context.Background()); initErr != nil {
				_, _ = ioStreams.ErrOut.Write([]byte("Warning: failed to initialize metrics: " + initErr.Error() + "\n"))
			}

			// Parse the resolved log level string to a slog level
			logLevel, parseErr := logger.ParseLogLevel(resolvedLogLevel)
			if parseErr != nil {
				_, _ = ioStreams.ErrOut.Write([]byte("Warning: " + parseErr.Error() + ", defaulting to 'none'\n"))
				logLevel = logger.LogLevelNone
			}

			// Map format string to logger.LogFormat
			var logFmt logger.LogFormat
			switch resolvedFormat {
			case config.LoggingFormatJSON:
				logFmt = logger.FormatJSON
			case config.LoggingFormatText, config.LoggingFormatConsole:
				logFmt = logger.FormatConsole
			default:
				logFmt = logger.FormatConsole
			}

			// Build logger options
			logOpts := logger.Options{
				Level:      logLevel,
				Timestamps: cfg.Logging.Timestamps,
				Format:     logFmt,
				FilePath:   resolvedLogFile,
				AlsoStderr: resolvedLogFile != "" && debugFlag,
			}

			lgr := logger.GetWithOptions(logOpts)

			// Create centralized writer and input, then attach to context.
			// Uses the same ioStreams instance passed to subcommand constructors.
			w := writer.New(ioStreams, cliParams, writerOpts...)
			in := input.New(ioStreams, cliParams)
			ctx := context.Background()
			ctx = logger.WithLogger(ctx, lgr)
			ctx = writer.WithWriter(ctx, w)
			ctx = input.WithInput(ctx, in)
			ctx = config.WithConfig(ctx, cfg)

			// Initialize auth registry with Entra handler
			authRegistry := auth.NewRegistry()
			var entraOpts []entra.Option
			if cfg.Auth.Entra != nil {
				entraOpts = append(entraOpts, entra.WithConfig(&entra.Config{
					ClientID:      cfg.Auth.Entra.ClientID,
					TenantID:      cfg.Auth.Entra.TenantID,
					DefaultScopes: cfg.Auth.Entra.DefaultScopes,
				}))
			}
			entraOpts = append(entraOpts, entra.WithLogger(*lgr))
			entraHandler, err := entra.New(entraOpts...)
			if err != nil {
				lgr.V(1).Info("warning: failed to initialize Entra auth handler", "error", err)
			} else {
				if regErr := authRegistry.Register(entraHandler); regErr != nil {
					lgr.V(1).Info("warning: failed to register Entra auth handler", "error", regErr)
				}
			}

			// Initialize GitHub auth handler
			var ghOpts []ghauth.Option
			if cfg.Auth.GitHub != nil {
				ghOpts = append(ghOpts, ghauth.WithConfig(&ghauth.Config{
					ClientID:      cfg.Auth.GitHub.ClientID,
					Hostname:      cfg.Auth.GitHub.Hostname,
					DefaultScopes: cfg.Auth.GitHub.DefaultScopes,
				}))
			}
			ghOpts = append(ghOpts, ghauth.WithLogger(*lgr))
			ghHandler, err := ghauth.New(ghOpts...)
			if err != nil {
				lgr.V(1).Info("warning: failed to initialize GitHub auth handler", "error", err)
			} else {
				if regErr := authRegistry.Register(ghHandler); regErr != nil {
					lgr.V(1).Info("warning: failed to register GitHub auth handler", "error", regErr)
				}
			}

			// Initialize GCP auth handler
			var gcpOpts []gcpauth.Option
			if cfg.Auth.GCP != nil {
				gcpOpts = append(gcpOpts, gcpauth.WithConfig(&gcpauth.Config{
					ClientID:                  cfg.Auth.GCP.ClientID,
					ClientSecret:              cfg.Auth.GCP.ClientSecret,
					DefaultScopes:             cfg.Auth.GCP.DefaultScopes,
					ImpersonateServiceAccount: cfg.Auth.GCP.ImpersonateServiceAccount,
					Project:                   cfg.Auth.GCP.Project,
				}))
			}
			gcpOpts = append(gcpOpts, gcpauth.WithLogger(*lgr))
			gcpHandler, err := gcpauth.New(gcpOpts...)
			if err != nil {
				lgr.V(1).Info("warning: failed to initialize GCP auth handler", "error", err)
			} else {
				if regErr := authRegistry.Register(gcpHandler); regErr != nil {
					lgr.V(1).Info("warning: failed to register GCP auth handler", "error", regErr)
				}
			}

			ctx = auth.WithRegistry(ctx, authRegistry)

			cCmd.SetContext(ctx)

			// Only validate args for the root command itself, not subcommands
			if cCmd.Use == "scafctl" {
				err := output.ValidateCommands(args)
				if err != nil {
					w.ErrorWithExit(err.Error())
					return
				}
			}

			// Unhide pprof flags if profiling is enabled in config
			if cfg.Logging.EnableProfiling {
				_ = cCmd.PersistentFlags().MarkHidden("pprof")                   // First try to set hidden to ensure it exists
				cCmd.PersistentFlags().Lookup("pprof").Hidden = false            //nolint:staticcheck // intentional
				cCmd.PersistentFlags().Lookup("pprof-output-dir").Hidden = false //nolint:staticcheck // intentional
			}

			if cCmd.Flags().Changed("pprof") {
				profileType, _ := cCmd.Flags().GetString("pprof")
				profilePath, _ := cCmd.Flags().GetString("pprof-output-dir")
				p, err := profiler.GetProfiler(profileType, profilePath, lgr)
				if err != nil {
					w.ErrorWithExitf("Error starting profiler: %v", err)
					return
				}

				go func() {
					e := p.Start(lgr)
					if e != nil {
						lgr.V(1).Info("Error starting profiler", "error", e)
						w.Errorf("Error starting profiler: %v", e)
						return
					}
				}()
			}
		},
		PersistentPostRun: func(_ *cobra.Command, _ []string) {
			if telShutdown != nil {
				shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = telShutdown(shutCtx)
			}
		},
		Annotations: map[string]string{
			"commandType": "main",
		},
	}

	cCmd.PersistentFlags().StringVar(&cliParams.MinLogLevel, "log-level", "none", "Set the log level (none, error, warn, info, debug, trace, or a numeric V-level)")
	cCmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "d", false, "Enable debug logging (shorthand for --log-level debug)")
	cCmd.PersistentFlags().StringVar(&logFormat, "log-format", "console", "Set the log output format (console, json)")
	cCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "Write logs to a file instead of stderr")
	cCmd.PersistentFlags().BoolVarP(&cliParams.IsQuiet, "quiet", "q", false, "Do not print additional information")
	cCmd.PersistentFlags().BoolVar(&cliParams.NoColor, "no-color", false, "Disable color output")
	cCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to config file (default: ~/.scafctl/config.yaml)")
	cCmd.PersistentFlags().String("pprof", "", "Enable profiling (options: memory, cpu)")
	cCmd.PersistentFlags().String("pprof-output-dir", "./", "directory path to save the profiler.prof file (default: current working directory)")
	cCmd.PersistentFlags().String("otel-endpoint", "", "OpenTelemetry OTLP exporter endpoint (e.g. localhost:4317). Overrides OTEL_EXPORTER_OTLP_ENDPOINT")
	cCmd.PersistentFlags().BoolVar(&otelInsecure, "otel-insecure", false, "Disable TLS for OTLP gRPC connection (development only)")

	if err := cCmd.PersistentFlags().MarkHidden("pprof"); err != nil {
		return nil
	}
	if err := cCmd.PersistentFlags().MarkHidden("pprof-output-dir"); err != nil {
		return nil
	}
	cCmd.AddCommand(version.CommandVersion(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(get.CommandGet(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(run.CommandRun(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(render.CommandRender(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(explain.CommandExplain(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(resolver.CommandResolver(cliParams, *ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(snapshot.CommandSnapshot(cliParams, *ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(configcmd.CommandConfig(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(secretscmd.CommandSecrets(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(authcmd.CommandAuth(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(lint.CommandLint(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(eval.CommandEval(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(newcmd.CommandNew(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(examplescmd.CommandExamples(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(build.CommandBuild(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(catalogcmd.CommandCatalog(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(cachecmd.CommandCache(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(bundlecmd.CommandBundle(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(vendorcmd.CommandVendor(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(testcmd.CommandTest(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(mcpcmd.CommandMCP(cliParams, ioStreams, settings.CliBinaryName))
	return cCmd
}

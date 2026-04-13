// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/entra"
	gcpauth "github.com/oakwood-commons/scafctl/pkg/auth/gcp"
	ghauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
	customoauth2 "github.com/oakwood-commons/scafctl/pkg/auth/oauth2"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	authcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/build"
	bundlecmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/bundle"
	cachecmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/cache"
	catalogcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/catalog"
	configcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/config"
	credhelpercmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/credentialhelper"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/eval"
	examplescmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/examples"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/explain"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get"
	inspectcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/inspect"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/lint"
	mcpcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/mcp"
	newcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/new"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/options"
	pluginscmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/plugins"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/render"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/run"
	secretscmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/secrets"
	servecmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/serve"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/snapshot"
	solutioncmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/solution"
	testcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/test"
	vendorcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/vendor"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/version"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/profiler"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// rootUsageTemplate is a custom usage template that hides global (persistent)
// flags from all commands and instead directs users to "scafctl options".
// This is based on Cobra's default usage template with the inherited-flags
// section replaced by a one-line pointer, matching kubectl's UX pattern.
// Local flags (command-specific) are still shown on subcommands.
const rootUsageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasParent}}{{if .NonInheritedFlags.HasAvailableFlags}}

Flags:
{{.NonInheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
Use "{{.Root.Name}} options" for a list of global command-line options (applies to all commands).
`

// Command group IDs for organizing subcommands in help output.
const (
	groupCore     = "core"
	groupInspect  = "inspect"
	groupScaffold = "scaffold"
	groupConfig   = "config"
	groupPlugin   = "plugin"
	groupServer   = "server"
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

	// BinaryName overrides the default CLI binary name used by this command
	// tree: root command Use field, subcommand Short descriptions, env var
	// prefix, telemetry service name, and version output. Solution discovery
	// and cache paths are wired through settings.Run.BinaryName.
	// When empty, defaults to settings.CliBinaryName ("scafctl").
	BinaryName string

	// PreRunHook is called within PersistentPreRun after scafctl's standard
	// setup (config, logger, auth, telemetry) is complete but before the
	// command's own RunE executes. The context on cmd is fully initialized.
	// When nil, no additional hook runs.
	PreRunHook func(cmd *cobra.Command, args []string) error

	// VersionExtra is additional version info displayed alongside scafctl's
	// own version. When non-nil, the version command shows both the
	// embedder's and scafctl's versions.
	VersionExtra *settings.VersionInfo

	// ConfigDefaults is an optional YAML byte slice providing embedder-level
	// configuration defaults. These are merged after scafctl's built-in
	// defaults but before the user's config file, environment variables,
	// and CLI flags.
	//
	// Merge precedence (lowest to highest):
	//  1. scafctl built-in defaults (setDefaults)
	//  2. ConfigDefaults (this field)
	//  3. User config file (~/.config/<binary>/config.yaml)
	//  4. Environment variables
	//  5. CLI flags
	ConfigDefaults []byte

	// AuthPluginDirs specifies directories to scan for auth handler plugin
	// binaries. Discovered plugins are registered in the auth.Registry
	// alongside built-in handlers during PersistentPreRunE.
	AuthPluginDirs []string
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
		configPath        = opts.ConfigPath
		cwdFlag           string
		debugFlag         bool
		logFormat         = "console"
		logFile           string
		otelInsecure      bool
		telShutdown       func(context.Context) error
		authPluginClients []*plugin.AuthHandlerClient
	)

	// Resolve binary name: use caller-provided or default to settings.CliBinaryName ("scafctl").
	binaryName := settings.CliBinaryName
	if opts.BinaryName != "" {
		binaryName = settings.SanitizeBinaryName(opts.BinaryName)
	}
	envPrefix := settings.SafeEnvPrefix(binaryName)
	cliParams.BinaryName = binaryName
	paths.SetAppName(binaryName)

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
		Use:   binaryName,
		Short: "A configuration discovery and scaffolding tool",
		Long: heredoc.Doc(`
			A configuration discovery and scaffolding tool
		`),
		SilenceUsage:  false,
		SilenceErrors: true,
		PersistentPreRun: func(cCmd *cobra.Command, args []string) {
			// Load configuration first (before logger setup so config can influence log level)
			// Build config manager options from RootOptions.
			var configOpts []config.ManagerOption
			if len(opts.ConfigDefaults) > 0 {
				configOpts = append(configOpts, config.WithBaseConfig(opts.ConfigDefaults))
			}
			if envPrefix != config.EnvPrefix {
				configOpts = append(configOpts, config.WithEnvPrefix(envPrefix))
			}
			mgr := config.NewManager(configPath, configOpts...)
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
				if envLevel := os.Getenv(envPrefix + "_LOG_LEVEL"); envLevel != "" {
					resolvedLogLevel = envLevel
				} else if envDebug := os.Getenv(envPrefix + "_DEBUG"); envDebug != "" && envDebug != "0" && envDebug != "false" {
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
				if envFormat := os.Getenv(envPrefix + "_LOG_FORMAT"); envFormat != "" {
					resolvedFormat = envFormat
				} else if cfg.Logging.Format != "" {
					resolvedFormat = cfg.Logging.Format
				}
			}

			// Resolve log file with precedence: flag > env > default (empty = stderr)
			resolvedLogFile := logFile
			if !cCmd.Flags().Changed("log-file") {
				if envPath := os.Getenv(envPrefix + "_LOG_PATH"); envPath != "" {
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
			serviceName := binaryName
			if cfg.Telemetry.ServiceName != "" {
				serviceName = cfg.Telemetry.ServiceName
			}
			telShutdown, err = telemetry.Setup(context.Background(), telemetry.Options{
				ServiceName:      serviceName,
				ServiceVersion:   settings.VersionInformation.BuildVersion,
				ExporterEndpoint: otelEndpoint,
				ExporterInsecure: resolvedOtelInsecure,
				SamplerType:      cfg.Telemetry.SamplerType,
				SamplerArg:       cfg.Telemetry.SamplerArg,
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
			ctx = config.WithManagerOptions(ctx, configOpts)

			// ── Resolve --cwd flag and inject into context ──
			// This must happen before any path resolution so that downstream
			// commands see the correct logical working directory.
			if cwdFlag != "" {
				absCwd, cwdErr := provider.ValidateDirectory(cwdFlag)
				if cwdErr != nil {
					w.ErrorWithExit(fmt.Sprintf("--cwd: %v", cwdErr))
					return
				}
				ctx = provider.WithWorkingDirectory(ctx, absCwd)
			}

			// ── Initialize CEL subsystem from config ──
			celValues := cfg.CEL.ToCELValues()
			celexp.InitFromAppConfig(ctx, celexp.CELConfigInput{
				CacheSize:          celValues.CacheSize,
				CostLimit:          celValues.CostLimit,
				UseASTBasedCaching: celValues.UseASTBasedCaching,
				EnableMetrics:      celValues.EnableMetrics,
			})

			// ── Initialize Go template cache from config ──
			gtValues := cfg.GoTemplate.ToGoTemplateValues()
			gotmpl.InitFromAppConfig(ctx, gotmpl.GoTemplateConfigInput{
				CacheSize:         gtValues.CacheSize,
				EnableMetrics:     gtValues.EnableMetrics,
				AllowEnvFunctions: gtValues.AllowEnvFunctions,
			})

			// Initialize shared secrets store with config-aware settings.
			// Auth handlers that receive this store will not create their own.
			sharedSecretStore, secretErr := secrets.New(
				secrets.WithRequireSecureKeyring(cfg.Settings.RequireSecureKeyring),
				secrets.WithLogger(*lgr),
			)
			if secretErr != nil {
				lgr.V(1).Info("shared secrets store unavailable; auth handlers will create their own", "error", secretErr)
			}

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
			if secretErr == nil {
				entraOpts = append(entraOpts, entra.WithSecretStore(sharedSecretStore))
			}
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
					ClientID:             cfg.Auth.GitHub.ClientID,
					ClientSecret:         cfg.Auth.GitHub.ClientSecret,
					Hostname:             cfg.Auth.GitHub.Hostname,
					DefaultScopes:        cfg.Auth.GitHub.DefaultScopes,
					AppID:                cfg.Auth.GitHub.AppID,
					InstallationID:       cfg.Auth.GitHub.InstallationID,
					PrivateKey:           cfg.Auth.GitHub.PrivateKey,
					PrivateKeyPath:       cfg.Auth.GitHub.PrivateKeyPath,
					PrivateKeySecretName: cfg.Auth.GitHub.PrivateKeySecretName,
				}))
			}
			ghOpts = append(ghOpts, ghauth.WithLogger(*lgr))
			if secretErr == nil {
				ghOpts = append(ghOpts, ghauth.WithSecretStore(sharedSecretStore))
			}
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
			if secretErr == nil {
				gcpOpts = append(gcpOpts, gcpauth.WithSecretStore(sharedSecretStore))
			}
			gcpHandler, err := gcpauth.New(gcpOpts...)
			if err != nil {
				lgr.V(1).Info("warning: failed to initialize GCP auth handler", "error", err)
			} else {
				if regErr := authRegistry.Register(gcpHandler); regErr != nil {
					lgr.V(1).Info("warning: failed to register GCP auth handler", "error", regErr)
				}
			}

			// Register custom OAuth2 handlers from config
			for _, customCfg := range cfg.Auth.CustomOAuth2 {
				if validateErr := customoauth2.ValidateConfig(customCfg); validateErr != nil {
					lgr.V(1).Info("warning: skipping invalid custom OAuth2 handler", "name", customCfg.Name, "error", validateErr)
					continue
				}
				if authRegistry.Has(customCfg.Name) {
					lgr.V(1).Info("warning: custom OAuth2 handler name conflicts with built-in handler, skipping", "name", customCfg.Name)
					continue
				}
				var customOpts []customoauth2.Option
				customOpts = append(customOpts, customoauth2.WithLogger(*lgr))
				if secretErr == nil {
					customOpts = append(customOpts, customoauth2.WithSecretStore(sharedSecretStore))
				}
				customHandler, err := customoauth2.New(customCfg, customOpts...)
				if err != nil {
					lgr.V(1).Info("warning: failed to initialize custom OAuth2 handler", "name", customCfg.Name, "error", err)
				} else {
					if regErr := authRegistry.Register(customHandler); regErr != nil {
						lgr.V(1).Info("warning: failed to register custom OAuth2 handler", "name", customCfg.Name, "error", regErr)
					}
				}
			}

			// Register auth handler plugins if directories are configured
			if len(opts.AuthPluginDirs) > 0 {
				lgr.V(1).Info("loading auth handler plugins", "dirs", opts.AuthPluginDirs)
				pluginCfg := &plugin.ProviderConfig{
					Quiet:      cliParams.IsQuiet,
					NoColor:    cliParams.NoColor,
					BinaryName: binaryName,
				}
				authClients, authPluginErr := plugin.RegisterAuthHandlerPlugins(ctx, authRegistry, opts.AuthPluginDirs, pluginCfg)
				if authPluginErr != nil {
					w.Warningf("failed to load some auth handler plugins: %v", authPluginErr)
				}
				authPluginClients = authClients
			}

			ctx = auth.WithRegistry(ctx, authRegistry)

			cCmd.SetContext(ctx)

			// Only validate args for the root command itself, not subcommands
			if cCmd.Use == binaryName {
				err := output.ValidateCommands(args)
				if err != nil {
					plugin.KillAllAuthHandlers(authPluginClients)
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

			// Call embedder's pre-run hook after all standard setup is complete.
			if opts.PreRunHook != nil {
				if hookErr := opts.PreRunHook(cCmd, args); hookErr != nil {
					plugin.KillAllAuthHandlers(authPluginClients)
					w.ErrorWithExit(hookErr.Error())
					return
				}
			}

			if cCmd.Flags().Changed("pprof") {
				profileType, _ := cCmd.Flags().GetString("pprof")
				profilePath, _ := cCmd.Flags().GetString("pprof-output-dir")
				p, err := profiler.GetProfiler(profileType, profilePath, lgr)
				if err != nil {
					plugin.KillAllAuthHandlers(authPluginClients)
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
			plugin.KillAllAuthHandlers(authPluginClients)
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

	cCmd.SetUsageTemplate(rootUsageTemplate)

	// Command groups — organizes subcommands into logical categories
	// in the help output, similar to kubectl's grouped help display.
	cCmd.AddGroup(
		&cobra.Group{ID: groupCore, Title: "Core Commands:"},
		&cobra.Group{ID: groupInspect, Title: "Inspection Commands:"},
		&cobra.Group{ID: groupScaffold, Title: "Scaffolding Commands:"},
		&cobra.Group{ID: groupConfig, Title: "Configuration & Security Commands:"},
		&cobra.Group{ID: groupPlugin, Title: "Plugin Commands:"},
		&cobra.Group{ID: groupServer, Title: "Server Commands:"},
	)

	cCmd.PersistentFlags().StringVar(&cliParams.MinLogLevel, "log-level", "none", "Set the log level (none, error, warn, info, debug, trace, or a numeric V-level)")
	cCmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "d", false, "Enable debug logging (shorthand for --log-level debug)")
	cCmd.PersistentFlags().StringVar(&logFormat, "log-format", "console", "Set the log output format (console, json)")
	cCmd.PersistentFlags().StringVar(&logFile, "log-file", "", "Write logs to a file instead of stderr")
	cCmd.PersistentFlags().BoolVarP(&cliParams.IsQuiet, "quiet", "q", false, "Do not print additional information")
	cCmd.PersistentFlags().BoolVar(&cliParams.Verbose, "verbose", false, "Enable verbose output across all commands")
	cCmd.PersistentFlags().BoolVar(&cliParams.NoColor, "no-color", false, "Disable color output")
	cCmd.PersistentFlags().StringVarP(&cwdFlag, "cwd", "C", "", "Change the working directory before executing the command (similar to git -C)")
	cCmd.PersistentFlags().StringVar(&configPath, "config", "", fmt.Sprintf("Path to config file (default: $XDG_CONFIG_HOME/%s/config.yaml or ~/.config/%s/config.yaml)", binaryName, binaryName))
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
	// Core Commands — primary workflows
	cCmd.AddCommand(withGroup(groupCore, run.CommandRun(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupCore, render.CommandRender(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupCore, lint.CommandLint(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupCore, testcmd.CommandTest(cliParams, ioStreams, binaryName)))

	// Inspection Commands — explore and understand solutions
	cCmd.AddCommand(withGroup(groupInspect, get.CommandGet(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupInspect, explain.CommandExplain(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupInspect, eval.CommandEval(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupInspect, inspectcmd.CommandInspect(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupInspect, solutioncmd.CommandSolution(cliParams, *ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupInspect, snapshot.CommandSnapshot(cliParams, *ioStreams, binaryName)))

	// Scaffolding Commands — create and package artifacts
	cCmd.AddCommand(withGroup(groupScaffold, newcmd.CommandNew(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupScaffold, build.CommandBuild(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupScaffold, bundlecmd.CommandBundle(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupScaffold, vendorcmd.CommandVendor(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupScaffold, catalogcmd.CommandCatalog(cliParams, ioStreams, binaryName)))

	// Configuration & Security Commands
	cCmd.AddCommand(withGroup(groupConfig, configcmd.CommandConfig(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupConfig, secretscmd.CommandSecrets(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupConfig, authcmd.CommandAuth(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupConfig, cachecmd.CommandCache(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupConfig, credhelpercmd.CommandCredentialHelper(cliParams, ioStreams, binaryName)))

	// Plugin Commands
	cCmd.AddCommand(withGroup(groupPlugin, pluginscmd.CommandPlugins(cliParams, ioStreams, binaryName)))
	cCmd.AddCommand(withGroup(groupPlugin, mcpcmd.CommandMCP(cliParams, ioStreams, binaryName)))

	// Server Commands
	cCmd.AddCommand(withGroup(groupServer, servecmd.CommandServe(cliParams, ioStreams, binaryName)))

	// Other Commands (no group — shown under "Additional Commands:")
	cCmd.AddCommand(version.CommandVersion(cliParams, ioStreams, binaryName, opts.VersionExtra))
	cCmd.AddCommand(examplescmd.CommandExamples(cliParams, ioStreams, binaryName))
	cCmd.AddCommand(options.CommandOptions(cliParams, ioStreams, binaryName))
	return cCmd
}

// withGroup sets the GroupID on a command and returns it for chaining.
func withGroup(group string, cmd *cobra.Command) *cobra.Command {
	cmd.GroupID = group
	return cmd
}

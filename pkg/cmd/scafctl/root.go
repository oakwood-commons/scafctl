// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"context"
	"os"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/entra"
	authcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/auth"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/build"
	cachecmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/cache"
	catalogcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/catalog"
	configcmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/config"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/explain"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/get"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/lint"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/render"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/resolver"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/run"
	secretscmd "github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/secrets"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/snapshot"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/version"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/profiler"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	cliParams  = settings.NewCliParams()
	configPath string
	appConfig  *config.Config
)

// AppConfig returns the loaded application configuration.
// Returns nil if configuration has not been loaded yet.
func AppConfig() *config.Config {
	return appConfig
}

// Root creates and returns the root cobra.Command for the scafctl CLI tool.
// It sets up persistent flags, configures logging, handles profiler options,
// validates command arguments, and adds subcommands. The command provides
// configuration discovery and scaffolding functionality.
func Root() *cobra.Command {
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
				_, _ = os.Stderr.WriteString("Warning: failed to load config: " + err.Error() + "\n")
				// Continue with defaults
				cfg = &config.Config{}
			}
			appConfig = cfg

			// Apply config settings to cliParams (CLI flags take precedence)
			if !cCmd.Flags().Changed("no-color") {
				cliParams.NoColor = cfg.Settings.NoColor
			}
			if !cCmd.Flags().Changed("quiet") {
				cliParams.IsQuiet = cfg.Settings.Quiet
			}
			if !cCmd.Flags().Changed("log-level") {
				// Safe conversion with bounds check
				logLevel := cfg.Logging.Level
				if logLevel > 127 {
					logLevel = 127
				} else if logLevel < -128 {
					logLevel = -128
				}
				cliParams.MinLogLevel = int8(logLevel) //nolint:gosec // bounds checked above
			}

			// Build logger options from config
			logOpts := logger.Options{
				Level:      cliParams.MinLogLevel * -1,
				Timestamps: cfg.Logging.Timestamps,
				Format:     logger.FormatJSON,
			}
			if cfg.Logging.Format == config.LoggingFormatText {
				logOpts.Format = logger.FormatText
			}

			lgr := logger.GetWithOptions(logOpts)
			ioStreams := terminal.NewIOStreams(os.Stdin, os.Stdout, os.Stderr, true)

			// Create centralized writer and input, then attach to context
			w := writer.New(ioStreams, cliParams)
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
			entraHandler, err := entra.New(entraOpts...)
			if err != nil {
				lgr.V(1).Info("warning: failed to initialize Entra auth handler", "error", err)
			} else {
				if regErr := authRegistry.Register(entraHandler); regErr != nil {
					lgr.V(1).Info("warning: failed to register Entra auth handler", "error", regErr)
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
						lgr.V(1).Info("Error starting profiler", zap.Error(e))
						w.Errorf("Error starting profiler: %v", e)
						return
					}
				}()
			}
		},
		Annotations: map[string]string{
			"commandType": "main",
		},
	}

	ioStreams := terminal.NewIOStreams(os.Stdin, os.Stdout, os.Stderr, true)

	cCmd.PersistentFlags().Int8Var(&cliParams.MinLogLevel, "log-level", 0, "Set the minimum log level (-1=Debug, 0=Info, 1=Warn, 2=Error)")
	cCmd.PersistentFlags().BoolVarP(&cliParams.IsQuiet, "quiet", "q", false, "Do not print additional information")
	cCmd.PersistentFlags().BoolVar(&cliParams.NoColor, "no-color", false, "Disable color output")
	cCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to config file (default: ~/.scafctl/config.yaml)")
	cCmd.PersistentFlags().String("pprof", "", "Enable profiling (options: memory, cpu)")
	cCmd.PersistentFlags().String("pprof-output-dir", "./", "directory path to save the profiler.prof file (default: current working directory)")

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
	cCmd.AddCommand(build.CommandBuild(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(catalogcmd.CommandCatalog(cliParams, ioStreams, settings.CliBinaryName))
	cCmd.AddCommand(cachecmd.CommandCache(cliParams, ioStreams, settings.CliBinaryName))
	return cCmd
}

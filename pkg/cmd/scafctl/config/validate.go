// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ValidateOptions holds options for the config validate command.
type ValidateOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	File       string
}

// CommandValidate creates the 'config validate' command.
func CommandValidate(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ValidateOptions{}

	cCmd := &cobra.Command{
		Use:   "validate [file]",
		Short: "Validate a configuration file",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Validate a scafctl configuration file.

			Checks that the configuration file is valid YAML and conforms
			to the expected schema. Reports any errors found.

			By default, validates the config file at ~/.scafctl/config.yaml.
			Specify a file path to validate a different file.

			Examples:
			  # Validate default config
			  scafctl config validate

			  # Validate a specific file
			  scafctl config validate ./my-config.yaml

			  # Validate with verbose output
			  scafctl config validate --log-level -1
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			if lgr := logger.FromContext(cCmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			w := writer.FromContext(cCmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams

			if len(args) > 0 {
				opts.File = args[0]
			}

			// Get config path from parent command context
			if configFlag := cCmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	return cCmd
}

// Run executes the config validate command.
func (o *ValidateOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}
	lgr := logger.FromContext(ctx)

	// Determine file to validate
	filePath := o.File
	if filePath == "" {
		if o.ConfigPath != "" {
			filePath = o.ConfigPath
		} else {
			var err error
			filePath, err = paths.ConfigFile()
			if err != nil {
				err = fmt.Errorf("failed to determine config path: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.ConfigError)
			}
		}
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		err := fmt.Errorf("config file not found: %s", filePath)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	if lgr != nil {
		lgr.V(1).Info("Validating config file", "path", filePath)
	}

	// Load and validate using the manager
	mgr := appconfig.NewManager(filePath)
	cfg, err := mgr.Load()
	if err != nil {
		w.Errorf("Validation failed: %s\n", filePath)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	// Run additional validation
	if err := cfg.Validate(); err != nil {
		w.Errorf("Validation failed: %s\n", filePath)
		err = fmt.Errorf("validation error: %w", err)
		return exitcode.WithCode(err, exitcode.ValidationFailed)
	}

	w.Successf("Valid: %s\n", filePath)

	// Print summary
	w.Infof("  Version: %d\n", cfg.Version)
	w.Infof("  Catalogs: %d\n", len(cfg.Catalogs))
	if cfg.Settings.DefaultCatalog != "" {
		w.Infof("  Default catalog: %s\n", cfg.Settings.DefaultCatalog)
	}

	return nil
}

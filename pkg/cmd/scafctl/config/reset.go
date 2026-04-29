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

// ResetOptions holds options for the config reset command.
type ResetOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Force      bool
	All        bool
}

// CommandReset creates the 'config reset' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandReset(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ResetOptions{}

	cCmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset configuration to defaults",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Reset the configuration file to defaults.

			This removes the existing config file and re-creates it from the
			defaults, whether they are embedded or supplied by the embedder.
			User customizations will be lost.

			Use --all to also clear cache and data directories (catalogs,
			secrets, build cache, plugins, HTTP cache).

			Requires --force to confirm.

			Examples:
			  # Reset config file to defaults
			  scafctl config reset --force

			  # Reset everything: config, cache, and data
			  scafctl config reset --all --force
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.NoArgs,
		RunE: func(cCmd *cobra.Command, _ []string) error {
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

			if configFlag := cCmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				opts.ConfigPath = configFlag.Value.String()
			}

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	cCmd.Flags().BoolVar(&opts.Force, "force", false, "Confirm destructive reset")
	cCmd.Flags().BoolVar(&opts.All, "all", false, "Also clear cache and data directories")

	return cCmd
}

// Run executes the config reset command.
func (o *ResetOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	if !o.Force {
		err := fmt.Errorf("reset is destructive; pass --force to confirm")
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	configPath := o.ConfigPath
	if configPath == "" {
		var err error
		configPath, err = paths.ConfigFile()
		if err != nil {
			err = fmt.Errorf("failed to determine config path: %w", err)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.ConfigError)
		}
	}

	// Remove existing config file.
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		err = fmt.Errorf("removing config file: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	// Re-create from defaults. Use embedder defaults when available so
	// that an embedding CLI (e.g. cldctl) resets to its own defaults
	// rather than scafctl's built-in defaults.
	baseDefaults := appconfig.BaseDefaultsFromContext(ctx)
	var ensureErr error
	if len(baseDefaults) > 0 {
		ensureErr = appconfig.EnsureDefaultsWith(configPath, baseDefaults)
	} else {
		ensureErr = appconfig.EnsureDefaults(configPath)
	}
	if ensureErr != nil {
		err := fmt.Errorf("writing default config: %w", ensureErr)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	w.Successf("Reset config file: %s", configPath)

	if o.All {
		dirs := []struct {
			name string
			path string
		}{
			{"cache", paths.CacheDir()},
			{"data", paths.DataDir()},
			{"state", paths.StateDir()},
		}

		for _, d := range dirs {
			if err := os.RemoveAll(d.path); err != nil {
				err = fmt.Errorf("removing %s directory %s: %w", d.name, d.path, err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
			w.Successf("Cleared %s directory: %s", d.name, d.path)
		}
	}

	return nil
}

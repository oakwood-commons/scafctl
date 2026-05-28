// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// UnsetOptions holds options for the config unset command.
type UnsetOptions struct {
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Key        string
}

// CommandUnset creates the 'config unset' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandUnset(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &UnsetOptions{}

	cCmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Unset a configuration value",
		Long: heredoc.Doc(`
			Unset (remove) a configuration value by key.

			This resets the value to its default. Uses dot notation for nested values.

			Examples:
			  # Unset log level (resets to default 0)
			  scafctl config unset logging.level

			  # Unset default catalog
			  scafctl config unset settings.defaultCatalog
		`),
		Args: cobra.ExactArgs(1),
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
			opts.Key = args[0]

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

// Run executes the config unset command.
func (o *UnsetOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	mgr := appconfig.NewManager(o.ConfigPath)

	// Delete operates directly on the YAML file and does not require a
	// successful Load(). This allows unset to repair broken config files
	// that would fail validation during Load().
	removed, err := mgr.Delete(o.Key)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	if removed {
		w.Successf("Unset %s (removed from config file; defaults will apply)", o.Key)
	} else {
		w.Successf("Key %s was not present in user config (defaults already apply)", o.Key)
	}
	return nil
}

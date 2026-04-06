// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// GetOptions holds options for the config get command.
type GetOptions struct {
	BinaryName string
	IOStreams  *terminal.IOStreams
	CliParams  *settings.Run
	ConfigPath string
	Key        string

	flags.KvxOutputFlags
}

// CommandGet creates the 'config get' command.
//
//nolint:dupl // Cobra command boilerplate is intentionally similar across commands
func CommandGet(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &GetOptions{
		BinaryName: cliParams.BinaryName,
	}

	cCmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Get a specific configuration value by key.

			Uses dot notation for nested values (e.g., settings.noColor).

			Examples:
			  # Get log level
			  scafctl config get logging.level

			  # Get default catalog
			  scafctl config get settings.defaultCatalog

			  # Get all catalogs as JSON
			  scafctl config get catalogs -o json
		`), settings.CliBinaryName, cliParams.BinaryName),
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

	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)

	return cCmd
}

// Run executes the config get command.
func (o *GetOptions) Run(ctx context.Context) error {
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	mgr := appconfig.NewManager(o.ConfigPath, appconfig.ManagerOptionsFromContext(ctx)...)
	_, err := mgr.Load()
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.ConfigError)
	}

	value := mgr.Get(o.Key)
	if value == nil {
		err := fmt.Errorf("key %q not found in configuration", o.Key)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	return o.writeOutput(ctx, value)
}

func (o *GetOptions) writeOutput(ctx context.Context, data any) error {
	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName(o.BinaryName+" config get"),
	)
	kvxOpts.IOStreams = o.IOStreams

	return kvxOpts.Write(data)
}

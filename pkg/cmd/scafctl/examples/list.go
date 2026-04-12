// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	exampleslib "github.com/oakwood-commons/scafctl/pkg/examples"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ListOptions holds options for the examples list command.
type ListOptions struct {
	IOStreams      *terminal.IOStreams
	CliParams      *settings.Run
	KvxOutputFlags flags.KvxOutputFlags
	Category       string
}

// CommandList creates the 'examples list' command.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ListOptions{}

	cCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List available example configurations",
		Long: heredoc.Doc(`
			List all available scafctl example configurations.

			Examples are organized by category: solutions, resolvers, actions, providers, etc.
			Use --category to filter by a specific category.

			Examples:
			  # List all examples
			  scafctl examples list

			  # Filter by category
			  scafctl examples list --category solutions

			  # Output as JSON
			  scafctl examples list -o json

			  # List available categories
			  scafctl examples list --category ""
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Use)
			ctx := settings.IntoContext(cmd.Context(), cliParams)

			if lgr := logger.FromContext(cmd.Context()); lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx = writer.WithWriter(ctx, w)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)
	cCmd.Flags().StringVar(&opts.Category, "category", "", "Filter by category (e.g., solutions, resolvers, actions)")

	return cCmd
}

// Run executes the examples list command.
func (o *ListOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	items, err := exampleslib.Scan(o.Category)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		if o.Category != "" {
			w.Infof("No examples found in category %q.", o.Category)
			cats := exampleslib.Categories()
			if len(cats) > 0 {
				w.Infof("Available categories: %s", strings.Join(cats, ", "))
			}
		} else {
			w.Infof("No examples available.")
		}
		return nil
	}

	outputOpts := flags.ToKvxOutputOptions(&o.KvxOutputFlags, kvx.WithIOStreams(o.IOStreams))
	return outputOpts.Write(items)
}

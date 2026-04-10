// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	exampleslib "github.com/oakwood-commons/scafctl/pkg/examples"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// GetOptions holds options for the examples get command.
type GetOptions struct {
	IOStreams *terminal.IOStreams
	CliParams *settings.Run
	Path      string
	Output    string // output file path (empty = stdout)
}

// CommandGet creates the 'examples get' command.
func CommandGet(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &GetOptions{}

	cCmd := &cobra.Command{
		Use:   "get <example-path>",
		Short: "Get the contents of an example",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Retrieve and display the contents of an example configuration file.

			The example-path is the relative path shown by 'scafctl examples list'.

			Examples:
			  # Display an example
			  scafctl examples get solutions/email-notifier/solution.yaml

			  # Save an example to a file
			  scafctl examples get solutions/email-notifier/solution.yaml -o my-solution.yaml

			  # View a provider example
			  scafctl examples get resolvers/cel-basics.yaml
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			opts.Path = args[0]

			return opts.Run(ctx)
		},
		SilenceUsage: true,
	}

	cCmd.Flags().StringVarP(&opts.Output, "output", "o", "", "Write example to file instead of stdout")

	return cCmd
}

// Run executes the examples get command.
func (o *GetOptions) Run(ctx context.Context) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	content, err := exampleslib.Read(o.Path)
	if err != nil {
		w.Errorf("Example not found: %s", o.Path)
		return exitcode.WithCode(fmt.Errorf("failed to read example: %w", err), exitcode.FileNotFound)
	}

	if o.Output != "" {
		if err := os.WriteFile(o.Output, []byte(content), 0o600); err != nil {
			return fmt.Errorf("failed to write to %q: %w", o.Output, err)
		}
		w.Successf("Example saved to %s", o.Output)
		return nil
	}

	// Write to stdout
	w.Plain(content)
	return nil
}

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// resolversSection is the state document section that holds persisted resolver
// values -- the entries the state provider can read back on later runs.
const resolversSection = "resolvers"

// listOptions holds the options for the list command.
type listOptions struct {
	flags.KvxOutputFlags
	Path string
}

// CommandList creates the 'state list' command. It lists the persisted resolver
// values held in a state file -- the entries accessible to the state provider on
// subsequent runs. Use 'state show' to view the full document (metadata,
// command, parameters, resolvers, fingerprints).
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &listOptions{
		KvxOutputFlags: flags.KvxOutputFlags{AppName: cliParams.BinaryName},
	}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List persisted resolver values",
		Long: "List the persisted resolver values stored in a state file -- the entries " +
			"the state provider can read back on later runs. Use 'state show' to view the " +
			"full state document.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			sd, err := loadStateForDisplay(w, opts.Path)
			if err != nil {
				return err
			}

			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))

			// Structured/interactive output, or any CEL filter (-e/--expression,
			// -w/--where), operates on the resolvers map so callers can traverse
			// or filter it (e.g. '_.current_token.value'). Normalize to native
			// map/slice types first because CEL cannot introspect a raw Go struct.
			if wantsStructuredOutput(&opts.KvxOutputFlags, kvxOpts.Format) {
				normalized, err := kvx.StructToMap(sd)
				if err != nil {
					err = fmt.Errorf("failed to prepare state for output: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				return kvxOpts.Write(scopeToSection(normalized, resolversSection))
			}

			// Human-readable output: render just the resolvers section as a table.
			view := state.BuildListView(sd)
			resolvers := view.SectionByName(resolversSection)
			if resolvers == nil || len(resolvers.Rows) == 0 {
				if !cliParams.IsQuiet {
					w.Warning("State file has no persisted resolver values")
				}
				return nil
			}
			return writeSection(w, kvxOpts, *resolvers)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)
	cmd.Flags().StringVar(&opts.Path, "path", "", "State file path (relative to working directory or absolute)")
	_ = cmd.MarkFlagRequired("path")

	return cmd
}

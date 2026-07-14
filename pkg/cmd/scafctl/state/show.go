// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// showOptions holds the options for the show command.
type showOptions struct {
	flags.KvxOutputFlags
	Path string
}

// CommandShow creates the 'state show' command. It displays the full contents
// of a state file -- schema version, metadata, command, parameters, resolvers,
// and fingerprints -- grouped by section. Use 'state list' to see just the
// persisted resolver values.
func CommandShow(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &showOptions{
		KvxOutputFlags: flags.KvxOutputFlags{AppName: cliParams.BinaryName},
	}

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the full contents of a state file",
		Long: "Show the full contents of a state file, grouped by section (metadata, " +
			"command, parameters, resolvers, fingerprints), led by a compact summary " +
			"header. Use 'state list' to see just the persisted resolver values.",
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
			// -w/--where), operates on the whole state document in a single write
			// so the filter sees the complete object (e.g.
			// '_.resolvers.current_token.value'). Normalize to native map/slice
			// types first because CEL cannot introspect a raw Go struct.
			if wantsStructuredOutput(&opts.KvxOutputFlags, kvxOpts.Format) {
				normalized, err := kvx.StructToMap(sd)
				if err != nil {
					err = fmt.Errorf("failed to prepare state for output: %w", err)
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				return kvxOpts.Write(normalized)
			}

			// Human-readable output: summary header followed by each populated
			// section. The view is built by reflecting over the state schema, so
			// it mirrors the state file layout and adapts to schema changes.
			return writeFullView(w, kvxOpts, sd, cliParams.IsQuiet)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)
	cmd.Flags().StringVar(&opts.Path, "path", "", "State file path (relative to working directory or absolute)")
	_ = cmd.MarkFlagRequired("path")

	return cmd
}

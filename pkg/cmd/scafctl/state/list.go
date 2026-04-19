// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// listOptions holds the options for the list command.
type listOptions struct {
	flags.KvxOutputFlags
	Path string
}

// CommandList creates the 'state list' command.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &listOptions{
		KvxOutputFlags: flags.KvxOutputFlags{AppName: cliParams.BinaryName},
	}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored state keys",
		Long:  "List all keys and metadata in a state file.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			if opts.Path == "" {
				err := fmt.Errorf("--path is required")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			sd, err := state.LoadFromFile(opts.Path)
			if err != nil {
				err := fmt.Errorf("failed to load state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			if len(sd.Values) == 0 {
				if !cliParams.IsQuiet {
					w.Warning("No state entries found")
				}
				return nil
			}

			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))

			keys := make([]string, 0, len(sd.Values))
			for k := range sd.Values {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			data := make([]map[string]any, 0, len(sd.Values))
			for _, name := range keys {
				entry := sd.Values[name]
				data = append(data, map[string]any{
					"key":       name,
					"type":      entry.Type,
					"updatedAt": entry.UpdatedAt.Format("2006-01-02T15:04:05Z"),
					"immutable": entry.Immutable,
				})
			}

			return kvxOpts.Write(data)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)
	cmd.Flags().StringVar(&opts.Path, "path", "", "State file path (relative to state directory)")
	_ = cmd.MarkFlagRequired("path")

	return cmd
}

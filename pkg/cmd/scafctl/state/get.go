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

// getOptions holds the options for the get command.
type getOptions struct {
	flags.KvxOutputFlags
	Path string
	Key  string
}

// CommandGet creates the 'state get' command.
func CommandGet(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &getOptions{
		KvxOutputFlags: flags.KvxOutputFlags{AppName: cliParams.BinaryName},
	}

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get a state value",
		Long:  "Retrieve and display the value of a specific state key.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			sd, err := state.LoadFromFile(opts.Path)
			if err != nil {
				err := fmt.Errorf("failed to load state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			entry, ok := sd.Values[opts.Key]
			if !ok {
				err := fmt.Errorf("key %q not found in state", opts.Key)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}

			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))

			updatedAt := ""
			if !entry.UpdatedAt.IsZero() {
				updatedAt = entry.UpdatedAt.Format("2006-01-02T15:04:05Z")
			}

			data := []map[string]any{{
				"key":       opts.Key,
				"value":     entry.Value,
				"type":      entry.Type,
				"updatedAt": updatedAt,
				"immutable": entry.Immutable,
			}}

			return kvxOpts.Write(data)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)
	cmd.Flags().StringVar(&opts.Path, "path", "", "State file path (relative to state directory)")
	cmd.Flags().StringVar(&opts.Key, "key", "", "Key to retrieve")
	_ = cmd.MarkFlagRequired("path")
	_ = cmd.MarkFlagRequired("key")

	return cmd
}

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"

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
		Long:  "Retrieve and display the value of a specific state key from parameters or persisted resolvers.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			// Resolve and verify the file exists before loading.
			cwd, err := os.Getwd()
			if err != nil {
				err := fmt.Errorf("cannot determine working directory: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			resolved, err := state.ResolveStatePath(opts.Path, cwd)
			if err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			if _, statErr := os.Stat(resolved); os.IsNotExist(statErr) {
				err := fmt.Errorf("state file not found: %s", resolved)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}

			sd, err := state.LoadFromFile(opts.Path, cwd)
			if err != nil {
				err := fmt.Errorf("failed to load state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))

			// Check parameters first, then persisted resolvers
			if val, ok := sd.Parameters[opts.Key]; ok {
				data := []map[string]any{{
					"key":     opts.Key,
					"value":   val,
					"section": "parameters",
				}}
				return kvxOpts.Write(data)
			}

			if entry, ok := sd.Resolvers[opts.Key]; ok {
				createdAt := ""
				if !entry.CreatedAt.IsZero() {
					createdAt = entry.CreatedAt.Format("2006-01-02T15:04:05Z")
				}
				section := "persisted"
				if entry.Immutable {
					section = "immutable"
				}
				data := []map[string]any{{
					"key":       opts.Key,
					"value":     entry.Value,
					"type":      entry.Type,
					"section":   section,
					"immutable": entry.Immutable,
					"createdAt": createdAt,
				}}
				return kvxOpts.Write(data)
			}

			err = fmt.Errorf("key %q not found in state", opts.Key)
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		},
	}

	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)
	cmd.Flags().StringVar(&opts.Path, "path", "", "State file path (relative to working directory or absolute)")
	cmd.Flags().StringVar(&opts.Key, "key", "", "Key to retrieve")
	_ = cmd.MarkFlagRequired("path")
	_ = cmd.MarkFlagRequired("key")

	return cmd
}

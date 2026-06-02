// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"
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

			// Resolve and verify the file exists before loading.
			resolved, err := state.ResolveStatePath(opts.Path)
			if err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			if _, statErr := os.Stat(resolved); os.IsNotExist(statErr) {
				err := fmt.Errorf("state file not found: %s", resolved)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}

			sd, err := state.LoadFromFile(opts.Path)
			if err != nil {
				err := fmt.Errorf("failed to load state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			totalEntries := len(sd.Parameters) + len(sd.Immutables)
			if totalEntries == 0 {
				if !cliParams.IsQuiet {
					w.Warning("No state entries found")
				}
				return nil
			}

			kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags, kvx.WithIOStreams(ioStreams))

			data := make([]map[string]any, 0, totalEntries)

			// Parameters section
			paramKeys := make([]string, 0, len(sd.Parameters))
			for k := range sd.Parameters {
				paramKeys = append(paramKeys, k)
			}
			sort.Strings(paramKeys)

			for _, name := range paramKeys {
				data = append(data, map[string]any{
					"key":      name,
					"section":  "parameters",
					"value":    sd.Parameters[name],
					"readonly": false,
				})
			}

			// Immutables section
			immKeys := make([]string, 0, len(sd.Immutables))
			for k := range sd.Immutables {
				immKeys = append(immKeys, k)
			}
			sort.Strings(immKeys)

			for _, name := range immKeys {
				entry := sd.Immutables[name]
				createdAt := ""
				if !entry.CreatedAt.IsZero() {
					createdAt = entry.CreatedAt.Format("2006-01-02T15:04:05Z")
				}
				data = append(data, map[string]any{
					"key":       name,
					"section":   "immutables",
					"type":      entry.Type,
					"createdAt": createdAt,
					"readonly":  true,
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

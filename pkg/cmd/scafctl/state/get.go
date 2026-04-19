// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// CommandGet creates the 'state get' command.
func CommandGet(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var (
		path         string
		key          string
		outputFormat string
	)

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

			sd, err := state.LoadFromFile(path)
			if err != nil {
				err := fmt.Errorf("failed to load state: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			entry, ok := sd.Values[key]
			if !ok {
				err := fmt.Errorf("key %q not found in state", key)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.FileNotFound)
			}

			switch outputFormat {
			case "json":
				data, marshalErr := json.MarshalIndent(entry, "", "  ")
				if marshalErr != nil {
					return fmt.Errorf("failed to marshal entry: %w", marshalErr)
				}
				w.Plainln(string(data))
			case "yaml":
				data, marshalErr := yaml.Marshal(entry)
				if marshalErr != nil {
					return fmt.Errorf("failed to marshal entry: %w", marshalErr)
				}
				w.Plain(string(data))
			default:
				w.Plainf("%v\n", entry.Value)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "State file path (relative to state directory)")
	cmd.Flags().StringVar(&key, "key", "", "Key to retrieve")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (json, yaml)")
	_ = cmd.MarkFlagRequired("path")
	_ = cmd.MarkFlagRequired("key")

	return cmd
}
